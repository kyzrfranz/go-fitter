package llm_context

import (
	"fmt"
	"math"
	"sort"
	"time"

	v1 "github.com/kyzrfranz/go-fitter/api/v1"
	"github.com/kyzrfranz/go-fitter/pkg/aggregators"
)

func Convert(activities []v1.Activity, healthSeries map[string]*v1.HealthMetricSeries) v1.LLMContextResponse {

	// 1. Initialize Map
	dailyMap := make(map[string]*v1.DailyContext)

	// Helper to ensure day exists safely
	getDay := func(dateStr string) *v1.DailyContext {
		if dateStr == "Unknown" {
			return nil
		}
		if _, ok := dailyMap[dateStr]; !ok {
			dailyMap[dateStr] = &v1.DailyContext{
				Date:       dateStr,
				Activities: []v1.ActivityContext{},
				Health:     v1.HealthContext{},
			}
		}
		return dailyMap[dateStr]
	}

	totalLoad := 0

	// Track min/max dates to fill gaps later
	var minDate, maxDate time.Time
	hasDates := false

	updateRange := func(dStr string) {
		t, err := time.Parse("2006-01-02", dStr)
		if err == nil {
			if !hasDates {
				minDate, maxDate = t, t
				hasDates = true
			} else {
				if t.Before(minDate) {
					minDate = t
				}
				if t.After(maxDate) {
					maxDate = t
				}
			}
		}
	}

	// 2. Process Activities
	for _, act := range activities {
		summary := act.SessionSummary

		// Prefer the typed root field; fall back to legacy session-summary keys.
		var dateStr string
		if !act.StartTime.IsZero() {
			dateStr = act.StartTime.Format("2006-01-02")
		} else {
			dateStr = extractDateFromMap(summary, "start_time", "startTime")
		}
		if dateStr == "Unknown" {
			continue
		}
		updateRange(dateStr)

		// Metrics Extraction
		maxHr := act.Custom.MaxHR
		avgPwr := act.Custom.AvgPwr
		avgHr := act.Custom.AvgHR
		totalTime := act.Custom.TotalTime
		totalDist := act.Custom.TotalDistance
		np := act.Custom.NormalizedPower
		efficiency := act.Custom.Efficiency
		decoupling := act.Custom.Decoupling

		pacing := act.Custom.PacingTrend
		pacingString := pacing
		if decoupling > 0.05 {
			pacingString = fmt.Sprintf("%s (Drift %.1f%%)", pacing, decoupling*100)
		}

		sport := act.Sport
		if sport == "" || sport == "Unknown" {
			sport = aggregators.GetString(summary, "sport")
		}

		laps := make([]v1.Lap, len(act.Laps))
		for i, lap := range act.Laps {
			laps[i] = v1.ToLap(lap)
		}

		ac := v1.ActivityContext{
			Type:        sport,
			Duration:    fmt.Sprintf("%.0fm", totalTime/60),
			Distance:    fmt.Sprintf("%.1fkm", totalDist/1000),
			AvgHR:       int(avgHr),
			NormPwr:     int(np),
			Efficiency:  efficiency,
			PacingTrend: pacingString,
			Intensity:   getIntensity(avgHr),
			AvgPwr:      int(avgPwr),
			MaxHR:       int(maxHr),
			Decoupling:  decoupling,
			Laps:        laps,
		}

		day := getDay(dateStr)
		if day != nil {
			day.Activities = append(day.Activities, ac)
			totalLoad += int(np)
		}
	}

	// 3. Process Health Series
	type healthAgg struct {
		rhrSum      float64
		rhrCount    int
		hrvSum      float64
		hrvCount    int
		sleepSum    float64
		weightSum   float64
		weightCount int
		activeSum   float64
	}
	healthAggs := make(map[string]*healthAgg)

	getHealthAgg := func(d string) *healthAgg {
		if _, ok := healthAggs[d]; !ok {
			healthAggs[d] = &healthAgg{}
		}
		return healthAggs[d]
	}

	for _, series := range healthSeries {
		if series == nil {
			continue
		}

		for _, sample := range series.Samples {
			dateStr := extractDateFromMap(sample, "date", "Date")
			if dateStr == "Unknown" {
				continue
			}
			updateRange(dateStr)

			val := aggregators.GetFloat(sample, "qty")
			if val == 0 {
				val = aggregators.GetFloat(sample, "Avg")
			}

			agg := getHealthAgg(dateStr)

			switch series.Name {
			case "resting_heart_rate":
				if val > 0 {
					agg.rhrSum += val
					agg.rhrCount++
				}
			case "heart_rate_variability":
				if val > 0 {
					agg.hrvSum += val
					agg.hrvCount++
				}
			case "sleep_analysis":
				// Sleep data is often fragmented (naps, stages). We Sum them.
				sleepVal := aggregators.GetFloat(sample, "totalSleep")
				if sleepVal == 0 {
					sleepVal = val
				}
				// Fix: Some sources report seconds or minutes.
				// Heuristic: If > 24, it's likely minutes.
				if sleepVal > 24 {
					sleepVal = sleepVal / 60.0
				}
				agg.sleepSum += sleepVal
			case "weight_body_mass":
				if val > 0 {
					agg.weightSum += val
					agg.weightCount++
				}
			case "active_energy":
				// Calories are cumulative. We Sum them.
				agg.activeSum += val
			}
		}
	}

	// 3.5 Apply Aggregations to Final Context
	for dateStr, agg := range healthAggs {
		day := getDay(dateStr)
		if day == nil {
			continue
		}

		// Averages
		if agg.rhrCount > 0 {
			day.Health.RHR = int(agg.rhrSum / float64(agg.rhrCount))
		}
		if agg.hrvCount > 0 {
			day.Health.HRV = int(agg.hrvSum / float64(agg.hrvCount))
		}
		if agg.weightCount > 0 {
			day.Health.Weight = math.Round((agg.weightSum/float64(agg.weightCount))*100) / 100
		}
		// Sums
		day.Health.SleepHrs = math.Round(agg.sleepSum*10) / 10
		day.Health.ActiveCals = int(agg.activeSum)
	}

	// 4. Fill Gaps (Continuous Timeline)
	// This ensures "Rest Days" (days with no data) are explicitly present
	if hasDates {
		for d := minDate; !d.After(maxDate); d = d.AddDate(0, 0, 1) {
			dStr := d.Format("2006-01-02")
			if _, exists := dailyMap[dStr]; !exists {
				// Create an empty "Rest Day" entry
				dailyMap[dStr] = &v1.DailyContext{
					Date:       dStr,
					Activities: []v1.ActivityContext{}, // Explicit empty list
					Health:     v1.HealthContext{},
				}
			}
		}
	}

	// 5. Flatten & Sort
	var timeline []v1.DailyContext
	for _, d := range dailyMap {
		timeline = append(timeline, *d)
	}

	sort.Slice(timeline, func(i, j int) bool {
		return timeline[i].Date < timeline[j].Date
	})

	return v1.LLMContextResponse{
		TotalTrainingLoad: totalLoad,
		ActivityCount:     len(activities),
		Timeline:          timeline,
	}
}

func extractDateFromMap(m map[string]any, keys ...string) string {
	if m == nil {
		return "Unknown"
	}

	for _, key := range keys {
		if val, ok := m[key]; ok {
			// 1. Time Object
			if t, ok := val.(time.Time); ok {
				return t.Format("2006-01-02")
			}
			// 2. String (ISO or Simple)
			if s, ok := val.(string); ok {
				// "2023-01-01T12:00:00Z" -> "2023-01-01"
				if len(s) >= 10 {
					return s[:10]
				}
			}
		}
	}
	return "Unknown"
}

func getIntensity(hr float64) string {
	if hr > 175 {
		return "Z5"
	}
	if hr > 165 {
		return "Z4 Threshold"
	}
	if hr > 150 {
		return "Z3 Tempo"
	}
	return "Z1/Z2 Endurance"
}
