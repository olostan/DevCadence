// Package planner is the invoker-driven, validator-gated portfolio planner
// service (WP-M3D-1B). A caller-bound Invoker proposes alternative portfolios;
// this package builds the prompt, strictly decodes the untrusted output,
// assigns every identity, timestamp and provenance field itself, and gates each
// alternative through cognition.PortfolioValidator. Nothing here activates,
// persists, retries or selects an endpoint (DCI-123/124).
package planner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/cognition"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// Invoker performs one planning model invocation. The caller binds an eligible
// endpoint into it; the planner never selects one.
type Invoker interface {
	Invoke(ctx context.Context, inv Invocation) (InvocationResult, error)
}

// Invocation is the single request handed to an Invoker.
type Invocation struct {
	Prompt       string
	PromptDigest string
}

// InvocationResult is the model output plus the provenance the invoker reports.
type InvocationResult struct {
	Content    string
	EndpointID string
	DriverID   string
	ModelID    string
}

// ProjectCharacteristics are caller-supplied project facts included in the prompt.
type ProjectCharacteristics struct {
	Languages []string
	RiskTags  []string
}

// Request carries every fact the planner may use.
type Request struct {
	Inventory       *protocol.ResourceInventory
	MachineProfile  *protocol.MachineCapabilityProfile
	ContextProfiles map[string]*protocol.ContextProfile
	BudgetStates    map[string]*protocol.BudgetState
	ResourceStates  map[string]*protocol.ResourceState
	Policy          *cognition.ValidationPolicy
	Project         ProjectCharacteristics
	Intents         []protocol.RecommendationIntent
	Invoker         Invoker
	Clock           clock.Clock
}

// Outcome classifies a Plan call that did not return an error.
type Outcome string

const (
	OutcomeRecommended      Outcome = "recommended"
	OutcomeAllRejected      Outcome = "all_rejected"
	OutcomeNoPlanner        Outcome = "no_planner"
	OutcomeInvocationFailed Outcome = "invocation_failed"
	OutcomeMalformedOutput  Outcome = "malformed_output"
)

const (
	reasonProvenanceIncomplete = "planner_provenance_incomplete"
	reasonValidationFailed     = "validation_failed"
	reasonRecordInvalidPrefix  = "record_invalid: "

	noPlannerDetail    = "no planning endpoint available; deterministic-only"
	maxDetailBytes     = 256
	maxReasonBodyBytes = 512
	maxContentBytes    = 262144
	setIDDigestPrefix  = 16
)

// Rejected records an alternative that did not become an accepted recommendation.
type Rejected struct {
	Intent      protocol.RecommendationIntent
	Reason      string
	Diagnostics []cognition.PortfolioDiagnostic
}

// Result is the advisory outcome of one Plan call. Nothing in it is active.
type Result struct {
	Outcome         Outcome
	SetID           string
	InventoryDigest string
	PromptDigest    string
	Accepted        []*protocol.PortfolioRecommendation
	Rejected        []Rejected
	Detail          string
}

// Plan runs one planning round. It returns a non-nil error only for invalid
// arguments, context cancellation or a prompt-construction failure; every other
// situation is an Outcome.
func Plan(ctx context.Context, req Request) (*Result, error) {
	if req.Inventory == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "planner: inventory is required")
	}
	if req.Clock == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "planner: clock is required")
	}
	intents, err := canonicalizeIntents(req.Intents)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	invDigest := cognition.InventoryDigest(req.Inventory)
	if req.Invoker == nil {
		return &Result{Outcome: OutcomeNoPlanner, Detail: noPlannerDetail, InventoryDigest: invDigest}, nil
	}
	prompt, promptDigest, err := BuildPrompt(req)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "planner: build prompt")
	}
	res, invErr := req.Invoker.Invoke(ctx, Invocation{Prompt: prompt, PromptDigest: promptDigest})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if invErr != nil {
		return &Result{
			Outcome:         OutcomeInvocationFailed,
			Detail:          truncateUTF8(invErr.Error(), maxDetailBytes),
			InventoryDigest: invDigest,
			PromptDigest:    promptDigest,
		}, nil
	}
	alts, detail := decodeOutput(res.Content, intents)
	if detail != "" {
		return &Result{
			Outcome:         OutcomeMalformedOutput,
			Detail:          detail,
			InventoryDigest: invDigest,
			PromptDigest:    promptDigest,
		}, nil
	}

	now := req.Clock.Now().UTC()
	synthesizedAt := now.Format(time.RFC3339)
	setID := deriveSetID(invDigest, promptDigest, synthesizedAt)
	policy := cognition.DefaultValidationPolicy()
	if req.Policy != nil {
		policy = *req.Policy
	}
	provenanceOK := strings.TrimSpace(res.EndpointID) != "" && strings.TrimSpace(res.DriverID) != ""
	validator := cognition.NewPortfolioValidator()

	out := &Result{SetID: setID, InventoryDigest: invDigest, PromptDigest: promptDigest}
	for _, intent := range intents {
		alt, ok := alts[intent]
		if !ok {
			continue
		}
		if !provenanceOK {
			out.Rejected = append(out.Rejected, Rejected{Intent: intent, Reason: reasonProvenanceIncomplete})
			continue
		}
		if reason := blankReason(alt); reason != "" {
			out.Rejected = append(out.Rejected, Rejected{Intent: intent, Reason: reasonRecordInvalidPrefix + reason})
			continue
		}
		rec := buildRecommendation(alt, intent, setID, invDigest, promptDigest, synthesizedAt, res)
		if err := rec.Validate(); err != nil {
			out.Rejected = append(out.Rejected, Rejected{Intent: intent, Reason: reasonRecordInvalidPrefix + truncateUTF8(err.Error(), maxReasonBodyBytes)})
			continue
		}
		vr := validator.Validate(cognition.ValidationInput{
			Portfolio:               &rec.RecommendedPortfolio,
			Inventory:               req.Inventory,
			MachineProfile:          req.MachineProfile,
			ContextProfiles:         req.ContextProfiles,
			BudgetStates:            req.BudgetStates,
			ResourceStates:          req.ResourceStates,
			Policy:                  &policy,
			ExpectedInventoryDigest: invDigest,
			Clock:                   clock.NewFake(now, 0),
		})
		if !vr.Valid {
			out.Rejected = append(out.Rejected, Rejected{Intent: intent, Reason: reasonValidationFailed, Diagnostics: vr.Diagnostics})
			continue
		}
		out.Accepted = append(out.Accepted, rec)
	}
	if len(out.Accepted) >= 1 {
		out.Outcome = OutcomeRecommended
	} else {
		out.Outcome = OutcomeAllRejected
	}
	return out, nil
}

// buildRecommendation assigns every identity, timestamp, revision and
// provenance field in Go; planner-supplied values for them are overwritten.
func buildRecommendation(alt decodedAlternative, intent protocol.RecommendationIntent, setID, invDigest, promptDigest, synthesizedAt string, res InvocationResult) *protocol.PortfolioRecommendation {
	portfolio := alt.Portfolio
	portfolio.SchemaVersion = protocol.SchemaVersion1
	portfolio.PortfolioID = setID + "-" + string(intent)
	portfolio.Revision = 1
	portfolio.CreatedAt = synthesizedAt
	for i := range portfolio.Channels {
		if portfolio.Channels[i].SchemaVersion == "" {
			portfolio.Channels[i].SchemaVersion = protocol.SchemaVersion1
		}
	}
	for i := range portfolio.BudgetPools {
		if portfolio.BudgetPools[i].SchemaVersion == "" {
			portfolio.BudgetPools[i].SchemaVersion = protocol.SchemaVersion1
		}
	}
	return &protocol.PortfolioRecommendation{
		SchemaVersion:          protocol.SchemaVersion1,
		RecommendationID:       setID + "_" + string(intent),
		InventoryDigest:        invDigest,
		SynthesizedAt:          synthesizedAt,
		RecommendedPortfolio:   portfolio,
		Rationale:              alt.Rationale,
		ExplanatoryDiagnostics: []string{},
		CapabilityProvenance:   []string{},
		SetID:                  setID,
		Intent:                 intent,
		Tradeoffs:              alt.Tradeoffs,
		Confidence:             alt.Confidence,
		Planner: &protocol.PlannerProvenance{
			EndpointID:       res.EndpointID,
			DriverID:         res.DriverID,
			ModelID:          res.ModelID,
			InvocationDigest: promptDigest,
		},
	}
}

func deriveSetID(invDigest, promptDigest, synthesizedAt string) string {
	sum := sha256.Sum256([]byte(invDigest + "|" + promptDigest + "|" + synthesizedAt))
	return "set_" + hex.EncodeToString(sum[:])[:setIDDigestPrefix]
}

// truncateUTF8 returns the longest prefix of s of at most limit bytes that ends
// on a rune boundary.
func truncateUTF8(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	n := limit
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// blankReason reports the first whitespace-only rationale or tradeoff; the
// protocol record check does not trim rationale (recorded follow-up).
func blankReason(a decodedAlternative) string {
	if strings.TrimSpace(a.Rationale) == "" {
		return "rationale is blank"
	}
	for i, t := range a.Tradeoffs {
		if strings.TrimSpace(t) == "" {
			return "tradeoffs[" + strconv.Itoa(i) + "] is blank"
		}
	}
	return ""
}
