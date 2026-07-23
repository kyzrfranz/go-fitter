package v1

import (
	"encoding/json"
	"time"
)

type Activity struct {
	Id             string           `bson:"_id,omitempty" json:"id,omitempty"`
	Sport          string           `bson:"sport,omitempty" json:"sport,omitempty"`
	SubSport       string           `bson:"sub_sport,omitempty" json:"sub_sport,omitempty"`
	StartTime      time.Time        `bson:"start_time,omitempty" json:"start_time,omitempty"`
	Laps           []map[string]any `bson:"laps" json:"laps,omitempty"`
	SessionSummary map[string]any   `bson:"sessionSummary" json:"sessionSummary"`
	SportMesg      map[string]any   `bson:"sportMesg,omitempty" json:"sportMesg,omitempty"`
	Records        []map[string]any `bson:"records,omitempty" json:"records,omitempty"`
	Meta           ActivityMeta     `bson:"meta" json:"meta"`
	Source         Source           `bson:"source,omitempty" json:"source,omitempty"`
	Custom         Custom           `bson:"custom" json:"custom"`
}

type Source struct {
	Vendor     string    `bson:"vendor,omitempty" json:"vendor,omitempty"`
	Provider   string    `bson:"provider,omitempty" json:"provider,omitempty"`
	Path       string    `bson:"path,omitempty" json:"path,omitempty"`
	FileHash   string    `bson:"file_hash,omitempty" json:"file_hash,omitempty"`
	IngestTime time.Time `bson:"ingest_time,omitempty" json:"ingest_time,omitempty"`
}

type Custom struct {
	Efficiency      float64 `bson:"efficiency" json:"efficiency"`
	MaxHR           float64 `bson:"max_hr" json:"max_hr"`
	AvgHR           float64 `bson:"avg_hr" json:"avg_hr"`
	AvgPwr          float64 `bson:"avg_pwr" json:"avg_pwr"`
	AvgSpeed        float64 `bson:"avg_speed" json:"avg_speed"`
	TotalTime       float64 `bson:"total_time" json:"total_time"`
	TotalDistance   float64 `bson:"total_distance" json:"total_distance"`
	NormalizedPower float64 `bson:"normalized_power" json:"normalized_power"`
	Decoupling      float64 `bson:"decoupling" json:"decoupling"`
	PacingTrend     string  `bson:"pacing_trend" json:"pacing_trend"`
}

type ActivityMeta struct {
	Title       string `bson:"title" json:"title"`
	Description string `bson:"description" json:"description"`
	Source      string `bson:"source" json:"source"`
}

// ParseActivityMeta tolerantly decodes the meta sidecar JSON that ships
// alongside a .fit in vendor exports (RunGap, HealthFit, Garmin Connect, ...).
// Each vendor uses slightly different key names, so we try the common ones
// instead of binding to one strict schema.
func ParseActivityMeta(b []byte) ActivityMeta {
	if len(b) == 0 {
		return ActivityMeta{}
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		return ActivityMeta{}
	}
	return ActivityMeta{
		Title:       pickMetaString(raw, "title", "name", "activityName", "Title", "Name"),
		Description: pickMetaString(raw, "description", "notes", "comment", "Description", "Notes"),
		Source:      pickMetaString(raw, "source", "sourceApp", "app", "device", "Source"),
	}
}

func pickMetaString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

type HealthMetricSeries struct {
	Name    string                   `bson:"name" json:"name"`
	Units   string                   `bson:"units" json:"units"`
	Samples []map[string]interface{} `bson:"samples" json:"samples"`
}

type LLMContextResponse struct {
	ContextWindow     string         `json:"context_window"`
	TotalTrainingLoad int            `json:"total_training_load"`
	ActivityCount     int            `json:"activity_count"`
	Timeline          []DailyContext `json:"timeline"` // New unified list
}

// ActivityContext is a semantic summary of a single workout.
type ActivityContext struct {
	Date     string `json:"date"`
	Type     string `json:"type"`
	Duration string `json:"duration"`
	Distance string `json:"distance"`

	// Physiological Load
	AvgHR   int `json:"avg_hr"`
	MaxHR   int `json:"max_hr"`
	AvgPwr  int `json:"avg_pwr"`
	NormPwr int `json:"norm_pwr"`

	// AI-Ready Insights
	Efficiency  float64 `json:"efficiency_factor"`
	Decoupling  float64 `json:"decoupling"`
	PacingTrend string  `json:"pacing"`
	Intensity   string  `json:"intensity_zone"`

	Laps []Lap `json:"laps"`
}

type Lap struct {
	LapPower               int     `json:"Lap Power"`
	AvgFormPower           float64 `json:"avg_form_power"`
	AvgFractionalCadence   float64 `json:"avg_fractional_cadence"`
	AvgGarminStepLength    float64 `json:"avg_garmin_step_length"`
	AvgGarminVerticalRatio float64 `json:"avg_garmin_vertical_ratio"`
	AvgHeartRate           int     `json:"avg_heart_rate"`
	AvgLegSpringStiffness  float64 `json:"avg_leg_spring_stiffness"`
	AvgPower               int     `json:"avg_power"`
	AvgRunningCadence      int     `json:"avg_running_cadence"`
	AvgSpeed               float64 `json:"avg_speed"`
	AvgStepLength          float64 `json:"avg_step_length"`
	AvgStrydPower          float64 `json:"avg_stryd_power"`
	AvgTemperature         int     `json:"avg_temperature"`
	MaxHeartRate           int     `json:"max_heart_rate"`
	MaxPower               int     `json:"max_power"`
	MaxSpeed               float64 `json:"max_speed"`
	MaxTemperature         int     `json:"max_temperature"`
	MinTemperature         int     `json:"min_temperature"`
	NormalizedPower        int     `json:"normalized_power"`
	SwimStroke             int     `json:"swim_stroke"`
	TotalAscent            int     `json:"total_ascent"`
	TotalDescent           int     `json:"total_descent"`
	TotalDistance          int     `json:"total_distance"`
	TotalElapsedTime       float64 `json:"total_elapsed_time"`
	TotalMovingTime        float64 `json:"total_moving_time"`
}

func ToLap(m map[string]any) Lap {
	l := Lap{}
	b, _ := json.Marshal(m)
	_ = json.Unmarshal(b, &l)
	return l
}

type DailyContext struct {
	Date       string            `json:"date"`
	Activities []ActivityContext `json:"activities,omitempty"`
	Health     HealthContext     `json:"health,omitempty"`
}

type HealthContext struct {
	SleepHrs   float64 `json:"sleep_hours,omitempty"`
	HRV        int     `json:"hrv,omitempty"`
	RHR        int     `json:"rhr,omitempty"`
	Weight     float64 `json:"weight,omitempty"`
	ActiveCals int     `json:"active_cals,omitempty"`
}
