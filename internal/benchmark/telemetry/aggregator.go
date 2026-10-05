package telemetry

import (
	"sort"
	"time"
)

type groupKey struct {
	strategy   string
	capability string
}

// Aggregate computes aggregated telemetry grouped by (Strategy, Capability) (REQ-05, REQ-06, REQ-07, REQ-09).
// If snapshots is empty, it returns TotalSnapshots: 0, Groups: nil without error or division-by-zero.
func Aggregate(snapshots []RunTelemetrySnapshot) (*AggregatedReport, error) {
	if len(snapshots) == 0 {
		return &AggregatedReport{
			GeneratedAt:    time.Now().UTC(),
			TotalSnapshots: 0,
			Groups:         nil,
		}, nil
	}

	grouped := make(map[groupKey][]RunTelemetrySnapshot)
	for _, s := range snapshots {
		k := groupKey{strategy: s.Strategy, capability: s.Capability}
		grouped[k] = append(grouped[k], s)
	}

	keys := make([]groupKey, 0, len(grouped))
	for k := range grouped {
		keys = append(keys, k)
	}

	// Deterministic sorting: Strategy ascending, then Capability ascending
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].strategy != keys[j].strategy {
			return keys[i].strategy < keys[j].strategy
		}
		return keys[i].capability < keys[j].capability
	})

	groups := make([]AggregatedTelemetry, 0, len(keys))
	for _, k := range keys {
		snaps := grouped[k]
		runCount := len(snaps)

		var (
			acceptedCount       int
			seededCount         int
			detectedCount       int
			uncertainAccounting bool
			sumInitialTokens    int64
			sumPeakTokens       int64
			sumCumulativeTokens int64
			sumCachedTokens     int64
			sumDuration         time.Duration
		)

		for _, s := range snaps {
			if s.Accepted {
				acceptedCount++
			}
			if s.AccountingUncertain {
				uncertainAccounting = true
			}
			if s.DefectSeeded {
				seededCount++
				if s.DefectStatus == "detected" || s.DefectStatus == "prevented" {
					detectedCount++
				}
			}
			sumInitialTokens += s.InitialTokens
			sumPeakTokens += s.PeakResidentTokens
			sumCumulativeTokens += s.CumulativeInputTokens
			sumCachedTokens += s.CachedTokens
			sumDuration += s.Duration
		}

		firstPassRate := float64(acceptedCount) / float64(runCount)

		var cacheHitRatio float64
		if sumCumulativeTokens > 0 {
			cacheHitRatio = float64(sumCachedTokens) / float64(sumCumulativeTokens)
		}

		var (
			defectCatchRate           float64
			defectCatchRateApplicable bool
		)
		if seededCount == 0 {
			defectCatchRate = 1.0
			defectCatchRateApplicable = false
		} else {
			defectCatchRate = float64(detectedCount) / float64(seededCount)
			defectCatchRateApplicable = true
		}

		var resourceEfficiency ResourceEfficiency
		if acceptedCount == 0 {
			resourceEfficiency = ResourceEfficiency{
				Value:       0,
				IsUndefined: true,
			}
		} else {
			resourceEfficiency = ResourceEfficiency{
				Value:       float64(sumCumulativeTokens) / float64(acceptedCount),
				IsUndefined: false,
			}
		}

		groups = append(groups, AggregatedTelemetry{
			Strategy:                    k.strategy,
			Capability:                  k.capability,
			RunCount:                    runCount,
			AcceptedCount:               acceptedCount,
			FirstPassAcceptanceRate:     firstPassRate,
			DefectCatchRate:             defectCatchRate,
			DefectCatchRateApplicable:   defectCatchRateApplicable,
			CacheHitRatio:               cacheHitRatio,
			AvgInitialTokens:            float64(sumInitialTokens) / float64(runCount),
			AvgPeakResidentTokens:       float64(sumPeakTokens) / float64(runCount),
			AvgCumulativeInputTokens:    float64(sumCumulativeTokens) / float64(runCount),
			AvgDuration:                 time.Duration(int64(sumDuration) / int64(runCount)),
			ResourcePerAcceptedResult:   resourceEfficiency,
			ContainsUncertainAccounting: uncertainAccounting,
		})
	}

	return &AggregatedReport{
		GeneratedAt:    time.Now().UTC(),
		TotalSnapshots: len(snapshots),
		Groups:         groups,
	}, nil
}
