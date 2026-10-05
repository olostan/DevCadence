package telemetry

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// GenerateJSONReport serializes an AggregatedReport into pretty-printed JSON (REQ-08).
func GenerateJSONReport(report *AggregatedReport) ([]byte, error) {
	if report == nil {
		return nil, fmt.Errorf("cannot generate JSON report for nil report")
	}
	return json.MarshalIndent(report, "", "  ")
}

// GenerateMarkdownReport synthesizes an AggregatedReport into GitHub-flavored Markdown (REQ-08).
// Formats undefined resource efficiency as "N/A (no accepted runs)" and flags accounting uncertainty explicitly.
func GenerateMarkdownReport(report *AggregatedReport) (string, error) {
	if report == nil {
		return "", fmt.Errorf("cannot generate markdown report for nil report")
	}

	var sb strings.Builder
	sb.WriteString("# Empirical Benchmark Telemetry Report\n\n")
	sb.WriteString(fmt.Sprintf("- Generated: %s\n", report.GeneratedAt.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("- Total Snapshots: %d\n\n", report.TotalSnapshots))

	if len(report.Groups) == 0 {
		sb.WriteString("No telemetry groups recorded.\n")
		return sb.String(), nil
	}

	sb.WriteString("## Aggregated Performance by Strategy & Capability\n\n")
	sb.WriteString("| Strategy | Capability | Runs | Accepted | Acceptance Rate | Defect Catch Rate | Cache Hit Ratio | Avg Peak Tokens | Avg Input Tokens | Resource / Accepted | Uncertain? |\n")
	sb.WriteString("|---|---|---|---|---|---|---|---|---|---|---|\n")

	for _, g := range report.Groups {
		acceptanceRateStr := fmt.Sprintf("%.1f%%", g.FirstPassAcceptanceRate*100)

		var defectCatchStr string
		if !g.DefectCatchRateApplicable {
			defectCatchStr = "N/A (no defects)"
		} else {
			defectCatchStr = fmt.Sprintf("%.1f%%", g.DefectCatchRate*100)
		}

		cacheHitStr := fmt.Sprintf("%.1f%%", g.CacheHitRatio*100)

		var resourceEffStr string
		if g.ResourcePerAcceptedResult.IsUndefined {
			resourceEffStr = "N/A (no accepted runs)"
		} else {
			resourceEffStr = fmt.Sprintf("%.1f tokens", g.ResourcePerAcceptedResult.Value)
		}

		uncertainStr := "No"
		if g.ContainsUncertainAccounting {
			uncertainStr = "**YES (provisional)**"
		}

		sb.WriteString(fmt.Sprintf("| %s | %s | %d | %d | %s | %s | %s | %.0f | %.0f | %s | %s |\n",
			g.Strategy,
			g.Capability,
			g.RunCount,
			g.AcceptedCount,
			acceptanceRateStr,
			defectCatchStr,
			cacheHitStr,
			g.AvgPeakResidentTokens,
			g.AvgCumulativeInputTokens,
			resourceEffStr,
			uncertainStr,
		))
	}

	return sb.String(), nil
}
