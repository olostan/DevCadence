package gate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/olostan/DevCadence/internal/benchmark"
	"github.com/olostan/DevCadence/internal/benchmark/experiments"
	"github.com/olostan/DevCadence/internal/benchmark/telemetry"
	"github.com/olostan/DevCadence/internal/errs"
)

// EvaluateM4Gate performs pure, deterministic evaluation of the Milestone M4 gate (REQ-04, REQ-05, INV-01, INV-02).
// Pairwise evaluates capability tiers: for each capability tier C present, compares Strategy 4 (Hybrid 4-Layer) vs Strategy 1 (Full History).
func EvaluateM4Gate(
	report *telemetry.AggregatedReport,
	falsifications map[string]*experiments.FalsificationResult,
	criteria GateCriteria,
) (*GateEvaluationResult, error) {
	const kind = "EvaluateM4Gate"

	// 1. Validate report != nil. If criteria bounds invalid -> return nil, errs.CategoryInvalidArgument (REQ-08).
	if report == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "%s: aggregated report cannot be nil", kind)
	}
	if criteria.MinCompletedRuns < 1 {
		return nil, errs.New(errs.CategoryInvalidArgument, "%s: min_completed_runs must be >= 1, got %d", kind, criteria.MinCompletedRuns)
	}
	if criteria.MinDefectCatchRate < 0.0 || criteria.MinDefectCatchRate > 1.0 {
		return nil, errs.New(errs.CategoryInvalidArgument, "%s: min_defect_catch_rate must be between 0.0 and 1.0, got %f", kind, criteria.MinDefectCatchRate)
	}
	if criteria.MaxResourceRatioVersusBaseline <= 0.0 {
		return nil, errs.New(errs.CategoryInvalidArgument, "%s: max_resource_ratio_versus_baseline must be > 0.0, got %f", kind, criteria.MaxResourceRatioVersusBaseline)
	}

	evaluatedAt := time.Now().UTC()

	// Compute report digest for traceability
	var reportDigest string
	if reportJSON, err := json.Marshal(report); err == nil {
		h := sha256.Sum256(reportJSON)
		reportDigest = "sha256:" + hex.EncodeToString(h[:])
	}

	// 2. Insufficient completed runs check (REQ-05, INV-04)
	if len(report.Groups) == 0 || report.TotalSnapshots < criteria.MinCompletedRuns {
		return &GateEvaluationResult{
			Decision:            DecisionInconclusive,
			CriteriaEvaluations: []CriterionResult{},
			Criteria:            []CriterionResult{},
			Summary:             fmt.Sprintf("Insufficient completed runs: %d completed out of %d required", report.TotalSnapshots, criteria.MinCompletedRuns),
			Recommendations: []string{
				"Execute full benchmark campaign to collect required sample size before formal gate evaluation.",
			},
			AggregatedReport: report,
			ReportDigest:     reportDigest,
			EvaluatedAt:      evaluatedAt,
		}, nil
	}

	// 3. Collect distinct capability tiers present in the report
	tierMap := make(map[string]bool)
	for _, g := range report.Groups {
		if g.Capability != "" {
			tierMap[g.Capability] = true
		}
	}
	tiers := make([]string, 0, len(tierMap))
	for t := range tierMap {
		tiers = append(tiers, t)
	}
	sort.Strings(tiers)

	var criteriaEvaluations []CriterionResult
	var recommendations []string

	recordCriterion := func(name string, passed bool, threshold, observed float64, details string) {
		cr := CriterionResult{
			Name:      name,
			Passed:    passed,
			Threshold: threshold,
			Observed:  observed,
			Actual:    observed,
			Details:   details,
		}
		criteriaEvaluations = append(criteriaEvaluations, cr)
		if !passed && details != "" {
			recommendations = append(recommendations, details)
		}
	}

	// Helper to find a group by strategy and capability
	findGroup := func(strategy, capability string) *telemetry.AggregatedTelemetry {
		for i := range report.Groups {
			g := &report.Groups[i]
			if g.Strategy == strategy && g.Capability == capability {
				return g
			}
		}
		return nil
	}

	strat4 := string(benchmark.StrategyHybrid4Layer)
	strat1 := string(benchmark.StrategyFullHistory)

	// 4. Pairwise Tier Evaluation across each capability tier C
	for _, capClass := range tiers {
		s4Group := findGroup(strat4, capClass)
		s1Group := findGroup(strat1, capClass)

		// a. Quality (Defect Catch Rate) for Strategy 4
		if s4Group != nil && s4Group.DefectCatchRateApplicable {
			pass := s4Group.DefectCatchRate >= criteria.MinDefectCatchRate
			details := fmt.Sprintf("Strategy 4 catch rate in tier %s: %.1f%% (threshold >= %.1f%%)",
				capClass, s4Group.DefectCatchRate*100, criteria.MinDefectCatchRate*100)
			if !pass {
				details = fmt.Sprintf("Quality regression in tier %s: Strategy 4 defect catch rate %.1f%% is below threshold %.1f%%",
					capClass, s4Group.DefectCatchRate*100, criteria.MinDefectCatchRate*100)
			}
			recordCriterion("defect_catch_rate_"+capClass, pass, criteria.MinDefectCatchRate, s4Group.DefectCatchRate, details)
		}

		// b. Resource Efficiency: compare Strategy 4 vs Strategy 1 within the same tier
		if s4Group != nil && s1Group != nil {
			if s4Group.ResourcePerAcceptedResult.IsUndefined {
				// Strategy 4 has 0 accepted runs -> fail closed
				recordCriterion("resource_efficiency_"+capClass, false, criteria.MaxResourceRatioVersusBaseline, math.Inf(1),
					fmt.Sprintf("Resource efficiency failed in tier %s: Strategy 4 has 0 accepted runs (undefined resource per accepted result)", capClass))
			} else if s1Group.ResourcePerAcceptedResult.IsUndefined {
				// Baseline Strategy 1 has 0 accepted runs while Strategy 4 has accepted runs -> passes
				recordCriterion("resource_efficiency_"+capClass, true, criteria.MaxResourceRatioVersusBaseline, 0.0,
					fmt.Sprintf("Resource efficiency passed in tier %s: Strategy 4 accepted runs while baseline Strategy 1 has 0 accepted runs", capClass))
			} else {
				baselineVal := s1Group.ResourcePerAcceptedResult.Value
				if baselineVal <= 0 {
					baselineVal = 1.0
				}
				ratio := s4Group.ResourcePerAcceptedResult.Value / baselineVal
				pass := ratio <= criteria.MaxResourceRatioVersusBaseline
				details := fmt.Sprintf("Resource ratio in tier %s: %.2fx versus baseline Strategy 1 (threshold <= %.2fx)",
					capClass, ratio, criteria.MaxResourceRatioVersusBaseline)
				if !pass {
					details = fmt.Sprintf("Resource inefficiency in tier %s: Strategy 4 resource ratio %.2fx exceeds baseline threshold %.2fx",
						capClass, ratio, criteria.MaxResourceRatioVersusBaseline)
				}
				recordCriterion("resource_efficiency_"+capClass, pass, criteria.MaxResourceRatioVersusBaseline, ratio, details)
			}
		}

		// c. Peak Resident Tokens check (if threshold configured)
		if criteria.MaxResidentContextRatioBaseline > 0.0 && s4Group != nil && s1Group != nil && s1Group.AvgPeakResidentTokens > 0 {
			peakRatio := s4Group.AvgPeakResidentTokens / s1Group.AvgPeakResidentTokens
			pass := peakRatio <= criteria.MaxResidentContextRatioBaseline
			details := fmt.Sprintf("Peak resident context ratio in tier %s: %.2fx (threshold <= %.2fx)",
				capClass, peakRatio, criteria.MaxResidentContextRatioBaseline)
			if !pass {
				details = fmt.Sprintf("Peak resident context excess in tier %s: %.2fx exceeds threshold %.2fx",
					capClass, peakRatio, criteria.MaxResidentContextRatioBaseline)
			}
			recordCriterion("peak_resident_context_"+capClass, pass, criteria.MaxResidentContextRatioBaseline, peakRatio, details)
		}
	}

	// 5. Delegation Floor Evaluation
	if falsifications != nil {
		falsKeys := make([]string, 0, len(falsifications))
		for k := range falsifications {
			falsKeys = append(falsKeys, k)
		}
		sort.Strings(falsKeys)

		totalApplicable := 0
		falsifiedCount := 0

		for _, k := range falsKeys {
			fals := falsifications[k]
			if fals == nil || !fals.IsApplicable {
				continue
			}
			totalApplicable++
			if fals.HypothesisFalsified {
				falsifiedCount++
				recordCriterion("delegation_floor_"+k, false, 0.0, 1.0,
					fmt.Sprintf("Delegation floor hypothesis falsified for %s: %s", k, fals.Reason))
			} else {
				recordCriterion("delegation_floor_"+k, true, 0.0, 0.0,
					fmt.Sprintf("Delegation floor hypothesis satisfied for %s", k))
			}
		}

		// If MaxFalsificationRate check is evaluated as aggregate
		if criteria.MaxFalsificationRate >= 0.0 && totalApplicable > 0 && !criteria.RequireZeroFalsifications {
			falsRate := float64(falsifiedCount) / float64(totalApplicable)
			pass := falsRate <= criteria.MaxFalsificationRate
			details := fmt.Sprintf("Aggregate falsification rate: %.1f%% (threshold <= %.1f%%)",
				falsRate*100, criteria.MaxFalsificationRate*100)
			if !pass {
				details = fmt.Sprintf("Delegation falsification rate %.1f%% exceeds maximum threshold %.1f%%",
					falsRate*100, criteria.MaxFalsificationRate*100)
			}
			recordCriterion("aggregate_falsification_rate", pass, criteria.MaxFalsificationRate, falsRate, details)
		}
	}

	// 6. Final Decision logic (REQ-05, INV-02)
	allPassed := true
	for _, c := range criteriaEvaluations {
		if !c.Passed {
			allPassed = false
			break
		}
	}

	decision := DecisionGo
	summaryText := "All Milestone M4 gate criteria passed. Empirical evidence supports progressing to Milestone M5."

	if !allPassed {
		decision = DecisionRevise
		summaryText = fmt.Sprintf("Milestone M4 gate failed: %d criteria did not meet normative thresholds. Revision required before Milestone M5.", len(recommendations))
	}

	return &GateEvaluationResult{
		Decision:            decision,
		CriteriaEvaluations: criteriaEvaluations,
		Criteria:            criteriaEvaluations,
		Summary:             summaryText,
		Recommendations:     recommendations,
		AggregatedReport:    report,
		ReportDigest:        reportDigest,
		EvaluatedAt:         evaluatedAt,
	}, nil
}

// SynthesizeEvidenceReport generates a comprehensive publication-ready Markdown evidence report (REQ-06, INV-03).
func SynthesizeEvidenceReport(eval *GateEvaluationResult) (string, error) {
	if eval == nil {
		return "", errs.New(errs.CategoryInvalidArgument, "SynthesizeEvidenceReport: evaluation result cannot be nil")
	}

	var sb strings.Builder

	sb.WriteString("# Milestone M4 Empirical Evidence Report & Gate Evaluation\n\n")

	sb.WriteString("## Executive Summary\n\n")
	sb.WriteString(fmt.Sprintf("- **Evaluated At:** %s\n", eval.EvaluatedAt.Format(time.RFC3339)))
	if eval.ReportDigest != "" {
		sb.WriteString(fmt.Sprintf("- **Telemetry Report Digest:** `%s`\n", eval.ReportDigest))
	}
	sb.WriteString(fmt.Sprintf("- **Gate Decision:** **%s**\n", strings.ToUpper(string(eval.Decision))))
	sb.WriteString(fmt.Sprintf("- **Summary:** %s\n\n", eval.Summary))

	// Decision Badge / Callout
	switch eval.Decision {
	case DecisionGo:
		sb.WriteString("> [!IMPORTANT]\n")
		sb.WriteString("> **GATE DECISION: GO**\n")
		sb.WriteString("> All empirical criteria for Milestone M4 (Defect Catch Rate, Resource Efficiency, Delegation Floor) have passed.\n")
		sb.WriteString("> The Cognitive Invocation Compiler and 4-layer context architecture demonstrate empirical superiority over monolithic history.\n\n")
	case DecisionRevise:
		sb.WriteString("> [!WARNING]\n")
		sb.WriteString("> **GATE DECISION: REVISE**\n")
		sb.WriteString("> One or more empirical criteria failed. Architectural adjustments or contract refinements are required prior to Milestone M5.\n\n")
	case DecisionInconclusive:
		sb.WriteString("> [!NOTE]\n")
		sb.WriteString("> **GATE DECISION: INCONCLUSIVE**\n")
		sb.WriteString("> Insufficient runs were recorded to evaluate formal gate thresholds.\n\n")
	}

	// Uncertainty Disclosure (INV-03, REQ-06)
	hasUncertainty := false
	if eval.AggregatedReport != nil {
		for _, g := range eval.AggregatedReport.Groups {
			if g.ContainsUncertainAccounting {
				hasUncertainty = true
				break
			}
		}
	}
	if hasUncertainty {
		sb.WriteString("### Epistemic Accounting Disclosure\n\n")
		sb.WriteString("> [!CAUTION]\n")
		sb.WriteString("> **Provisional Token Accounting:** Certain telemetry runs were collected from endpoints without calibrated native token counters.\n")
		sb.WriteString("> Token counts for these runs represent heuristic approximations and are explicitly marked as provisional per DCI-005.\n\n")
	}

	// Quantitative Gate Criteria Evaluation Table
	sb.WriteString("## Gate Criteria Evaluation\n\n")
	if len(eval.CriteriaEvaluations) == 0 {
		sb.WriteString("No criteria evaluated.\n\n")
	} else {
		sb.WriteString("| Criterion | Status | Threshold | Observed | Details |\n")
		sb.WriteString("|---|---|---|---|---|\n")
		for _, c := range eval.CriteriaEvaluations {
			statusStr := "✅ PASS"
			if !c.Passed {
				statusStr = "❌ FAIL"
			}
			var threshStr string
			var obsStr string
			if strings.HasPrefix(c.Name, "defect_catch_rate") {
				threshStr = fmt.Sprintf(">= %.1f%%", c.Threshold*100)
				obsStr = fmt.Sprintf("%.1f%%", c.Observed*100)
			} else if strings.HasPrefix(c.Name, "resource_efficiency") {
				threshStr = fmt.Sprintf("<= %.2fx", c.Threshold)
				if math.IsInf(c.Observed, 1) {
					obsStr = "Undefined (0 accepted)"
				} else {
					obsStr = fmt.Sprintf("%.2fx", c.Observed)
				}
			} else if strings.HasPrefix(c.Name, "delegation_floor") {
				threshStr = "0 falsifications"
				if c.Passed {
					obsStr = "Satisfied"
				} else {
					obsStr = "Falsified"
				}
			} else {
				threshStr = fmt.Sprintf("%.2f", c.Threshold)
				obsStr = fmt.Sprintf("%.2f", c.Observed)
			}

			sb.WriteString(fmt.Sprintf("| `%s` | %s | %s | %s | %s |\n",
				c.Name, statusStr, threshStr, obsStr, c.Details))
		}
		sb.WriteString("\n")
	}

	// Recommendations
	if len(eval.Recommendations) > 0 {
		sb.WriteString("## Recommendations & Next Steps\n\n")
		for _, r := range eval.Recommendations {
			sb.WriteString(fmt.Sprintf("- %s\n", r))
		}
		sb.WriteString("\n")
	}

	// Underlying Telemetry Report
	if eval.AggregatedReport != nil {
		sb.WriteString("## Aggregated Telemetry Breakdown\n\n")
		mdReport, err := telemetry.GenerateMarkdownReport(eval.AggregatedReport)
		if err == nil {
			sb.WriteString(mdReport)
		}
	}

	return sb.String(), nil
}
