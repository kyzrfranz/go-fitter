package extractors

import (
	"math"

	v1 "github.com/kyzrfranz/go-fitter/api/v1"
	"github.com/kyzrfranz/go-fitter/pkg/aggregators"
)

func Efficiency(a v1.Activity) float64 {
	avgHR := AvgHeartRate(a)
	if avgHR <= 0 {
		return 0
	}
	if avgPower := AvgPower(a); avgPower > 0 {
		return math.Round((avgPower/avgHR)*100) / 100
	}
	if avgSpeed := AvgSpeed(a); avgSpeed > 0 {
		return math.Round((avgSpeed/avgHR)*100) / 100
	}
	return 0
}

func MaxHr(a v1.Activity) float64 {
	return aggregators.GetFloat(a.SessionSummary, "max_heart_rate")
}

func AvgPower(a v1.Activity) float64 {
	if a.SessionSummary["sport"] == "running" {
		return aggregators.GetFloat(a.SessionSummary, "avg_running_power")
	}
	return aggregators.GetFloat(a.SessionSummary, "avg_power")
}

func AvgHeartRate(a v1.Activity) float64 {
	return aggregators.GetFloat(a.SessionSummary, "avg_heart_rate")
}

func AvgSpeed(a v1.Activity) float64 {
	// FIT invalid sentinels (after scale=1000):
	//   avg_speed         uint16 -> 0xFFFF / 1000 = 65.535
	//   enhanced_avg_speed uint32 -> 0xFFFFFFFF / 1000 = 4294967.295
	const u16Sentinel = 65.535
	const u32Sentinel = 4294967.295

	valid := func(v float64) bool {
		return v > 0 && v < u16Sentinel
	}

	if v := aggregators.GetFloat(a.SessionSummary, "avg_speed"); valid(v) {
		return v
	}
	if v := aggregators.GetFloat(a.SessionSummary, "enhanced_avg_speed"); v > 0 && v < u32Sentinel {
		return v
	}
	return 0
}

func TotalTime(a v1.Activity) float64 {
	return aggregators.GetFloat(a.SessionSummary, "total_timer_time")
}

func TotalDistance(a v1.Activity) float64 {
	return aggregators.GetFloat(a.SessionSummary, "total_distance")
}
