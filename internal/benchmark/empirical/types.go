// Package empirical is the pure, offline admission contract for empirical M4
// re-evaluation evidence (WP-M5-5). It validates a campaign plan, operator
// authorization binding and post-run manifest, and replays the unchanged M4 gate
// only from independently verified outcomes. It performs no network, provider,
// credential or spending effect and ships no verifier or operator-receipt
// issuer: absence of either is a fail-closed denial, never a pass.
package empirical

import (
	"context"
	"time"

	"github.com/olostan/DevCadence/internal/protocol"
)

// SchemaVersion is the only accepted empirical-campaign schema version.
const SchemaVersion = "1.0"

// Closed vocabularies (no silent normalization).
const (
	StrategyFullHistory = "full_history"
	StrategyHybrid4     = "hybrid_4layer"

	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusBlocked   = "blocked"
	StatusCancelled = "cancelled"
	StatusSkipped   = "skipped"

	CauseAbsent        = "absent"
	CauseNotAuthorized = "not_authorized"
	CauseUnavailable   = "unavailable"

	QualityAccepted = "accepted"
	QualityRejected = "rejected"

	// UnknownQuotaUnit is the literal pinned when a provider exposes no quota meter.
	UnknownQuotaUnit = "unknown"
	// ProvenanceMeasured is the only provenance a Known measurement may carry.
	ProvenanceMeasured = "measured"
	// ProvenanceUnknown is the only provenance an unknown measurement may carry.
	ProvenanceUnknown = "unknown"
)

// Reason codes returned in AdmissionResult.ReasonCodes.
const (
	ReasonSchemaInvalid       = "SCHEMA_INVALID"
	ReasonArtifactUnavailable = "ARTIFACT_UNAVAILABLE"
	ReasonDigestMismatch      = "DIGEST_MISMATCH"
	ReasonPlanInvalid         = "PLAN_INVALID"
	ReasonPlanMismatch        = "PLAN_MISMATCH"
	ReasonAuthorizationBad    = "AUTHORIZATION_INVALID"
	ReasonOperatorAuthority   = "OPERATOR_AUTHORITY_UNAVAILABLE"
	ReasonRunMismatch         = "RUN_MISMATCH"
	ReasonRunInvalid          = "RUN_INVALID"
	ReasonMeasurementInvalid  = "MEASUREMENT_INVALID"
	ReasonSyntheticEvidence   = "SYNTHETIC_EVIDENCE"
	ReasonUnverifiedOutcome   = "UNVERIFIED_EMPIRICAL_OUTCOME"
	ReasonVerifierRejected    = "VERIFIER_REJECTED"
	ReasonNotIndependent      = "VERIFIER_NOT_INDEPENDENT"
	ReasonFalsificationBad    = "FALSIFICATION_INVALID"
)

// Measurement is one measured-or-unknown quantity. Unknown is never numeric zero.
type Measurement struct {
	Known       bool     `json:"known"`
	Value       *float64 `json:"value"`
	Unit        string   `json:"unit"`
	Provenance  string   `json:"provenance"`
	EvidenceRef string   `json:"evidence_ref"`
}

// EndpointBinding pins the endpoint/model/configuration of a run.
type EndpointBinding struct {
	EndpointID            string `json:"endpoint_id"`
	DriverID              string `json:"driver_id"`
	ModelID               string `json:"model_id"`
	ModelRevision         string `json:"model_revision"`
	ChannelID             string `json:"channel_id"`
	CapabilityClass       string `json:"capability_class"`
	RuntimeVersion        string `json:"runtime_version"`
	ContextProfileDigest  string `json:"context_profile_digest"`
	PolicyDigest          string `json:"policy_digest"`
	SubscriptionQuotaUnit string `json:"subscription_quota_unit"`
}

// RunEvidence is the produced, post-run record of one planned run.
type RunEvidence struct {
	RunID                   string                 `json:"run_id"`
	TaskID                  string                 `json:"task_id"`
	TaskDigest              string                 `json:"task_digest"`
	Seed                    string                 `json:"seed"`
	Strategy                string                 `json:"strategy"`
	Repetition              int                    `json:"repetition"`
	Endpoint                EndpointBinding        `json:"endpoint"`
	Status                  string                 `json:"status"`
	SnapshotRef             string                 `json:"snapshot_ref"`
	SnapshotDigest          string                 `json:"snapshot_digest"`
	SessionEvidenceRef      string                 `json:"session_evidence_ref"`
	SessionEvidenceDigest   string                 `json:"session_evidence_digest"`
	PromptDigest            string                 `json:"prompt_digest"`
	CandidateCommit         string                 `json:"candidate_commit"`
	CandidateArtifactRef    string                 `json:"candidate_artifact_ref"`
	CandidateArtifactDigest string                 `json:"candidate_artifact_digest"`
	VerifierReceiptRef      string                 `json:"verifier_receipt_ref"`
	VerifierReceiptDigest   string                 `json:"verifier_receipt_digest"`
	InvocationProducerID    string                 `json:"invocation_producer_id"`
	VerifierProducerID      string                 `json:"verifier_producer_id"`
	Accepted                bool                   `json:"accepted"`
	Measurements            map[string]Measurement `json:"measurements"`
}

// ClosedEWPBinding pins the closed EWP a run executes.
type ClosedEWPBinding struct {
	ID             string `json:"id"`
	Version        int    `json:"version"`
	RecordDigest   string `json:"record_digest"`
	ContractDigest string `json:"contract_digest"`
	BaseCommit     string `json:"base_commit"`
}

// PlannedRun is one authorized-to-be run in the pre-run plan.
type PlannedRun struct {
	Ordinal    int              `json:"ordinal"`
	RunID      string           `json:"run_id"`
	TaskID     string           `json:"task_id"`
	TaskDigest string           `json:"task_digest"`
	Seed       string           `json:"seed"`
	Strategy   string           `json:"strategy"`
	Repetition int              `json:"repetition"`
	Endpoint   EndpointBinding  `json:"endpoint"`
	EWP        ClosedEWPBinding `json:"ewp"`
}

// TierLimitation explains why a requested tier has no planned/produced matrix.
type TierLimitation struct {
	Tier        string `json:"tier"`
	Cause       string `json:"cause"`
	EvidenceRef string `json:"evidence_ref"`
}

// RunLimits is the proposed workload envelope (not authority).
type RunLimits struct {
	MaxTotalRuns                  int     `json:"max_total_runs"`
	MaxCallsPerRun                int     `json:"max_calls_per_run"`
	MaxTotalCalls                 int     `json:"max_total_calls"`
	MaxRunSeconds                 int     `json:"max_run_seconds"`
	MaxCampaignSeconds            int     `json:"max_campaign_seconds"`
	MaxAPISpendMicroUSD           int64   `json:"max_api_spend_micro_usd"`
	MaxSubscriptionCalls          int     `json:"max_subscription_calls"`
	MaxLocalComputeSeconds        float64 `json:"max_local_compute_seconds"`
	AllowMetered                  bool    `json:"allow_metered"`
	AllowUnknownSubscriptionQuota bool    `json:"allow_unknown_subscription_quota"`
}

// CampaignPlan is the sole pre-run authorized content. It carries no digest,
// authorization, result, session, receipt or timestamp field (acyclic identity).
type CampaignPlan struct {
	SchemaVersion             string           `json:"schema_version"`
	CampaignID                string           `json:"campaign_id"`
	SourceCommit              string           `json:"source_commit"`
	CorpusDigest              string           `json:"corpus_digest"`
	CriteriaDigest            string           `json:"criteria_digest"`
	VerifierSourceCommit      string           `json:"verifier_source_commit"`
	VerificationProfileDigest string           `json:"verification_profile_digest"`
	RequestedTiers            []string         `json:"requested_tiers"`
	MissingTiers              []TierLimitation `json:"missing_tiers"`
	Runs                      []PlannedRun     `json:"runs"`
	Limits                    RunLimits        `json:"limits"`
	AllowedSourceClasses      []string         `json:"allowed_source_classes"`
	AllowedNetworkDomains     []string         `json:"allowed_network_domains"`
	CredentialRefs            []string         `json:"credential_refs"`
}

// CampaignManifest is the independent immutable post-run content.
type CampaignManifest struct {
	SchemaVersion               string           `json:"schema_version"`
	CampaignID                  string           `json:"campaign_id"`
	PlanRef                     string           `json:"plan_ref"`
	PlanDigest                  string           `json:"plan_digest"`
	AuthorizationRef            string           `json:"authorization_ref"`
	AuthorizationDigest         string           `json:"authorization_digest"`
	MissingTiers                []TierLimitation `json:"missing_tiers"`
	Runs                        []RunEvidence    `json:"runs"`
	FalsificationEvidenceRef    string           `json:"falsification_evidence_ref"`
	FalsificationEvidenceDigest string           `json:"falsification_evidence_digest"`
	RegenerationCommand         []string         `json:"regeneration_command"`
}

// CampaignAuthorization is the operator-issued sidecar binding the exact plan
// digest and caps. Zero for a cap means no authority for that resource.
type CampaignAuthorization struct {
	Version                       string            `json:"version"`
	PlanDigest                    string            `json:"plan_digest"`
	AuthorizedBy                  string            `json:"authorized_by"`
	Expiry                        string            `json:"expiry"`
	AllowedEndpointBindings       []EndpointBinding `json:"allowed_endpoint_bindings"`
	AllowedSourceClasses          []string          `json:"allowed_source_classes"`
	AllowedNetworkDomains         []string          `json:"allowed_network_domains"`
	CredentialRefs                []string          `json:"credential_refs"`
	MaxTotalRuns                  int               `json:"max_total_runs"`
	MaxCallsPerRun                int               `json:"max_calls_per_run"`
	MaxTotalCalls                 int               `json:"max_total_calls"`
	MaxRunSeconds                 int               `json:"max_run_seconds"`
	MaxCampaignSeconds            int               `json:"max_campaign_seconds"`
	MaxAPISpendMicroUSD           int64             `json:"max_api_spend_micro_usd"`
	MaxSubscriptionCalls          int               `json:"max_subscription_calls"`
	MaxLocalComputeSeconds        float64           `json:"max_local_compute_seconds"`
	AllowMetered                  bool              `json:"allow_metered"`
	AllowUnknownSubscriptionQuota bool              `json:"allow_unknown_subscription_quota"`
	IssuedPolicyDigest            string            `json:"issued_policy_digest"`
}

// ArtifactResolver is digest-checked local retrieval with no implicit network.
// Admission re-hashes every returned byte slice and never trusts the resolver's
// own verification.
type ArtifactResolver interface {
	ReadVerified(ctx context.Context, ref, digest string) ([]byte, error)
}

// VerifiedOutcome is trusted in-process output of an independent verifier.
type VerifiedOutcome struct {
	RunID                       string
	PlanDigest                  string
	SessionDigest               string
	CandidateDigest             string
	SnapshotDigest              string
	ReceiptDigest               string
	VerifierSourceCommit        string
	VerificationProfileDigest   string
	Worker, Verifier            protocol.ActorProvenance
	QualityVerdict              string
	SeededDefectTotal           int
	SeededDefectsCaught         int
	VerifiedCommandArtifactRefs []string
}

// IndependentVerifier reruns pinned deterministic verification on the immutable
// candidate. No implementation ships in this package: it is a follow-on runtime
// prerequisite (M5-R3). Test fakes exercise control flow only.
type IndependentVerifier interface {
	Verify(ctx context.Context, plan CampaignPlan, run PlannedRun, evidence RunEvidence, resolver ArtifactResolver) (VerifiedOutcome, error)
}

// AuthorityWindow defines the validity window of a verified authorization grant
// (WP-M5-R3 Amendment 3). Times are UTC; NotAfter is already min(authorization.Expiry, receipt NotAfter).
type AuthorityWindow struct {
	IssuedAt time.Time
	NotAfter time.Time
}

// OperatorAuthority verifies that the authorization artifact was issued through
// protected operator authority (WP-M5-R3 Amendment 3, WP-M5-R4).
type OperatorAuthority interface {
	VerifyAuthorization(ctx context.Context, planDigest, authorizationDigest string, authorization []byte) (AuthorityWindow, error)
}

// RunCounts keeps attempted, completed, accepted, failed, blocked, cancelled
// and skipped runs separate (R3). Missing runs are planned but unreported; they
// are never counted as zero-valued results.
type RunCounts struct {
	Planned   int `json:"planned"`
	Reported  int `json:"reported"`
	Missing   int `json:"missing"`
	Attempted int `json:"attempted"`
	Completed int `json:"completed"`
	Accepted  int `json:"accepted"`
	Failed    int `json:"failed"`
	Blocked   int `json:"blocked"`
	Cancelled int `json:"cancelled"`
	Skipped   int `json:"skipped"`
}

// AdmissionResult is the outcome of ValidateAdmission. Admitted is true only
// after plan, authorization authority and every independent verification pass.
type AdmissionResult struct {
	Admitted                  bool      `json:"admitted"`
	ReasonCodes               []string  `json:"reason_codes"`
	CompletedRunIDs           []string  `json:"completed_run_ids"`
	ExcludedRunIDs            []string  `json:"excluded_run_ids"`
	Limitations               []string  `json:"limitations"`
	ComparableResourceMetrics []string  `json:"comparable_resource_metrics"`
	Counts                    RunCounts `json:"counts"`

	// admitted holds verified data for ReplayGate. Unexported so a result built
	// by a caller can never be replayed as empirical evidence.
	admitted *admittedSet
}
