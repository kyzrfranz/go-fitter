package extractors

import (
	v1 "github.com/kyzrfranz/go-fitter/api/v1"
	"github.com/kyzrfranz/go-fitter/pkg/aggregators"
)

func NP(a v1.Activity) float64 {
	np := AvgPower(a)
	if a.Records != nil && len(a.Records) > 30 {
		if calcNP := aggregators.NormalizedPower(a.Records); calcNP > 0 {
			np = calcNP
		}
	}
	return np
}

func Decoupling(a v1.Activity) float64 {
	return aggregators.Decoupling(a.Records)
}

func PacingTrend(a v1.Activity) string {
	return aggregators.PacingTrend(a.Records)
}
