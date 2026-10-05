package facade

import (
	"context"

	"github.com/olostan/DevCadence/internal/controlplane"
	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/protocol"
)

// CallMeta aliases the wire envelope for port signatures.
type CallMeta = principal.CallMeta

// CallerContext aliases the trusted caller for port signatures.
type CallerContext = principal.CallerContext

// CandidateRef and OperationRef alias the wire identities for port signatures.
type (
	CandidateRef   = principal.CandidateRef
	OperationRef   = principal.OperationRef
	WorkPackageRef = principal.WorkPackageRef
)

// Production implementations of SnippetWorker, Investigator, TaskExecutor and
// ReviewExecutor belong to accepted follow-on runtime work packages. A nil port
// is the production default and denies with MODEL_UNAVAILABLE before any
// effect. Spies prove dispatch and denial only; they are not execution.

// AuthorizedTask is what a TaskExecutor receives for delegation.
type AuthorizedTask struct {
	Caller      CallerContext
	Meta        CallMeta
	TaskID      string
	WorkPackage WorkPackageRef
}

// TaskExecutor starts delegation and validation. The accepted runtime owns the
// current policy and compare-and-set check before any effect.
type TaskExecutor interface {
	Delegate(context.Context, AuthorizedTask) (OperationRef, error)
	Validate(context.Context, CallerContext, CallMeta, CandidateRef, string) (OperationRef, error)
}

// ReviewExecutor starts an independent review.
type ReviewExecutor interface {
	Review(context.Context, CallerContext, CallMeta, CandidateRef, []string) (OperationRef, error)
}

// SnippetRequest asks the worker for one bounded, mediated source excerpt. The
// worker alone reads the authorized worktree, resolves symlinks canonically and
// verifies HitRef against project, base and path before releasing content.
type SnippetRequest struct {
	Meta                             CallMeta
	BaseCommit, Path, Reason, HitRef string
	StartLine, EndLine, MaxBytes     int
}

// DiffRequest asks the worker for one bounded hunk of an immutable candidate.
type DiffRequest struct {
	Meta                         CallMeta
	Candidate                    CandidateRef
	Path, Reason                 string
	StartLine, EndLine, MaxBytes int
}

// SnippetWorker is the only route to file content.
type SnippetWorker interface {
	Request(context.Context, CallerContext, SnippetRequest) (OperationRef, error)
	Diff(context.Context, CallerContext, DiffRequest) (OperationRef, error)
}

// EvidenceRecordRef names one exact durable record.
type EvidenceRecordRef struct {
	Kind, ID, Digest string
	Version          int
}

// SummaryRequest asks for a bounded semantic summary of a digest-verified record.
type SummaryRequest struct {
	Meta     CallMeta
	Record   EvidenceRecordRef
	MaxBytes int
}

// SummaryReader resolves an exact record in the batch project and builds an
// allowed bounded semantic projection; it never returns raw durable JSON.
type SummaryReader interface {
	ReadSummary(context.Context, CallerContext, SummaryRequest) (protocol.EvidencePacket, error)
}

// InvestigationRequest carries the question verbatim as untrusted data.
type InvestigationRequest struct {
	Meta                 CallMeta
	Question, BaseCommit string
	ScopePaths           []string
	MaxBytes             int
}

// Investigator is the scout runtime. Nil denies rather than ignoring Question.
type Investigator interface {
	Investigate(context.Context, CallerContext, InvestigationRequest) (protocol.EvidencePacket, error)
}

// EvidenceQuery is a search or symbol lookup.
type EvidenceQuery struct {
	Meta                                  CallMeta
	Kind, BaseCommit, Path, Query, Symbol string
	ScopePaths                            []string
	MaxMatches, MaxBytes                  int
}

// EvidenceReader serves search and symbol queries only; it never invokes a model.
type EvidenceReader interface {
	Read(context.Context, CallerContext, EvidenceQuery) (protocol.EvidencePacket, error)
}

// GateInput is the future acceptance gate's input.
type GateInput struct {
	Caller                   CallerContext
	Meta                     CallMeta
	Candidate                CandidateRef
	ValidationIDs, ReviewIDs []string
}

// CandidateGate is a reserved seam for a separately reviewed enabled
// acceptance contract. It is not activated: nothing in this package calls it.
type CandidateGate interface {
	Check(context.Context, controlplane.BatchReadView, GateInput) error
}

// PolicyResolver is required before every facade effect. Grant presence alone
// is insufficient: the resolver checks the same immutable launch binding and
// policy identity the caller was built from.
type PolicyResolver interface {
	Check(context.Context, CallerContext, CallMeta, string) error
}

// RepositoryObserver re-reads the registered repository now, uncached, so that
// parallel change made outside DevCadence is detected rather than served stale.
// Nil disables drift detection (a project with no registered repository).
type RepositoryObserver interface {
	// Drift compares the repository's current commit and working tree with
	// base. ChangedPaths are the files that differ between base and the
	// working tree (later commits plus uncommitted edits) restricted to
	// prefixes; an empty prefixes list means every path.
	Drift(ctx context.Context, base string, prefixes []string) (Drift, error)
}

// Drift is one live observation of the repository against a base commit.
type Drift struct {
	HeadCommit   string
	Dirty        bool
	ChangedPaths []string
}
