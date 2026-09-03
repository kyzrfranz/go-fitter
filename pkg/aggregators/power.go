package aggregators

import "math"

// maxPlausiblePower caps record-level power readings. Stryd-style sensors
// occasionally emit single-sample spikes well above any human output (and the
// FIT uint16 sentinel 65535 leaks through when a device drops a reading);
// both forms ruin NP and the chart. 2500W comfortably covers a track sprint
// while rejecting obvious garbage.
const maxPlausiblePower = 2500.0

// Power returns a sanitized instantaneous power reading from a record map.
// Invalid or implausible samples collapse to 0 so they don't contaminate
// downstream rolling averages.
func Power(r map[string]any) float64 {
	p := GetFloat(r, "power")
	if p == 0 {
		p = GetFloat(r, "Power")
	}
	if p <= 0 || p > maxPlausiblePower {
		return 0
	}
	return p
}

func NormalizedPower(records []map[string]any) float64 {
	if len(records) < 30 {
		return 0
	}

	powers := make([]float64, len(records))
	for i, r := range records {
		powers[i] = Power(r)
	}

	window := 30
	var rolling []float64
	currSum := 0.0

	for i := 0; i < window; i++ {
		currSum += powers[i]
	}
	rolling = append(rolling, currSum/float64(window))

	for i := window; i < len(powers); i++ {
		currSum = currSum - powers[i-window] + powers[i]
		rolling = append(rolling, currSum/float64(window))
	}

	sum4 := 0.0
	for _, val := range rolling {
		sum4 += math.Pow(val, 4)
	}

	avg4 := sum4 / float64(len(rolling))
	return math.Round(math.Pow(avg4, 0.25))
}

// Decoupling computes aerobic decoupling as a drift fraction (e.g. 0.05 = 5%).
// Power-based (Pw:HR) is preferred when records carry power; otherwise falls
// back to HR:pace (speed/HR). Returns 0 when neither signal is usable.
func Decoupling(records []map[string]any) float64 {
	if len(records) < 120 {
		return 0
	}
	mid := len(records) / 2
	first, second := records[:mid], records[mid:]

	if d, ok := powerDrift(first, second); ok {
		return d
	}
	if d, ok := paceDrift(first, second); ok {
		return d
	}
	return 0
}

// powerDrift returns Pw:HR decoupling and ok=true when power data is present
// in both halves. NP/HR is the canonical efficiency factor; drift = (EF1-EF2)/EF1.
func powerDrift(first, second []map[string]any) (float64, bool) {
	np1 := NormalizedPower(first)
	np2 := NormalizedPower(second)
	if np1 <= 0 || np2 <= 0 {
		return 0, false
	}
	hr1 := avgHR(first)
	hr2 := avgHR(second)
	if hr1 <= 0 || hr2 <= 0 {
		return 0, false
	}
	ef1 := np1 / hr1
	ef2 := np2 / hr2
	if ef1 == 0 {
		return 0, false
	}
	return (ef1 - ef2) / ef1, true
}

// paceDrift returns HR:pace decoupling using speed as the proxy for pace
// (speed and pace are inverses; drift sign is identical to the power case).
func paceDrift(first, second []map[string]any) (float64, bool) {
	sp1 := avgSpeed(first)
	sp2 := avgSpeed(second)
	if sp1 <= 0 || sp2 <= 0 {
		return 0, false
	}
	hr1 := avgHR(first)
	hr2 := avgHR(second)
	if hr1 <= 0 || hr2 <= 0 {
		return 0, false
	}
	ef1 := sp1 / hr1
	ef2 := sp2 / hr2
	if ef1 == 0 {
		return 0, false
	}
	return (ef1 - ef2) / ef1, true
}

func avgHR(records []map[string]any) float64 {
	var sum float64
	var n int
	for _, r := range records {
		if v := GetFloat(r, "heart_rate"); v > 0 {
			sum += v
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

func avgSpeed(records []map[string]any) float64 {
	var sum float64
	var n int
	for _, r := range records {
		v := GetFloat(r, "speed")
		if v == 0 {
			v = GetFloat(r, "enhanced_speed")
		}
		if v > 0 {
			sum += v
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

func PacingTrend(records []map[string]any) string {
	if len(records) < 120 {
		return "Steady"
	}

	halfway := len(records) / 2
	var sum1, sum2 float64
	var count1, count2 int

	for i, r := range records {
		val := GetFloat(r, "speed")
		if val == 0 {
			val = GetFloat(r, "enhanced_speed")
		}
		// Fallback to power if stationary
		if val == 0 {
			val = Power(r)
		}

		if i < halfway {
			sum1 += val
			count1++
		} else {
			sum2 += val
			count2++
		}
	}

	if count1 > 0 && count2 > 0 {
		avg1 := sum1 / float64(count1)
		avg2 := sum2 / float64(count2)
		if avg1 > 0 {
			ratio := avg2 / avg1
			if ratio > 1.05 {
				return "Negative Split"
			}
			if ratio < 0.95 {
				return "Positive Split"
			}
		}
	}
	return "Steady"
}
