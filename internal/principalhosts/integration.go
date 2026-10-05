package principalhosts

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/protocol"
)

// This file is WP-M5-4's host integration service. Host-specific keys live only
// here; the semantic contract is the identical devcadence-mcp stdio server for
// every host. Nothing here writes to a host's configuration: no enforced
// exclusive operator-owned mutation channel exists, so application is manual
// (see ApplyApproved).

// Host observation states.
const (
	StateAbsent       = "absent"
	StateDetected     = "detected"
	StateIncompatible = "incompatible"
	StateUnknown      = "unknown"
	StateAmbiguous    = "ambiguous"
)

// Integration modes.
const (
	ModeStrict   = "strict"
	ModeAssisted = "assisted"
)

// Verification states.
const (
	ReadyStrict   = "strict_ready"
	ReadyAssisted = "assisted_ready"
	Unverified    = "unverified"
	Blocked       = "blocked"
)

// PlanFormat versions the canonical plan digest.
const PlanFormat = "principal-host-plan/v1"

// Sentinel errors.
var (
	ErrInvalidRequest     = errors.New("principalhosts: invalid integration request")
	ErrHostNotSelected    = errors.New("principalhosts: host is not an explicit, detected selection")
	ErrGuidanceOnly       = errors.New("principalhosts: this host has guidance only in this slice")
	ErrUnauthorized       = errors.New("principalhosts: plan approval is missing, wrong or unverifiable")
	ErrStalePreimage      = errors.New("principalhosts: a planned file changed since the plan")
	ErrReverification     = errors.New("principalhosts: identities changed; reverification required")
	ErrConfigNotMergeable = errors.New("principalhosts: existing configuration cannot be merged safely")
)

//go:embed assets/antigravity-rule.md
var antigravityRule []byte

//go:embed assets/antigravity-skill.md
var antigravitySkill []byte

//go:embed assets/cursor-rule.mdc
var cursorRule []byte

// HostObservation is the bounded view of an M3 observation.
type HostObservation struct {
	HostID, Executable, Version, OS string
	ObservedAt                      time.Time
	State                           string
	EvidenceRefs                    []string
}

// IntegrationRequest is the operator's explicit selection and paths.
type IntegrationRequest struct {
	HostID, ProjectID, PrincipalWorkspace, MCPExecutable string
	RuntimeHome, PrincipalBindingPath                    string
	Mode                                                 string
}

// PlannedFile is one exact file the operator will apply.
type PlannedFile struct {
	Path, BeforeSHA256, AfterSHA256 string
	Content                         []byte
	OwnedEntry                      string
}

// IntegrationPlan is immutable and digest-bound.
type IntegrationPlan struct {
	Digest                 string
	Request                IntegrationRequest
	Observation            HostObservation
	Files                  []PlannedFile
	ManualSteps            []string
	ApprovalRequired       bool
	AutomaticApplyEligible bool
}

// ApprovedPlan names the plan and the approval reference authorizing it.
type ApprovedPlan struct{ PlanDigest, ApprovalRef string }

// ApplyReceipt reports an application attempt. It is evidence, not readiness.
type ApplyReceipt struct {
	PlanDigest, ApprovalRef   string
	AppliedPaths, FailedPaths []string
	Manual                    bool
	EvidenceRefs              []string
}

// ApprovalVerifier checks an approval reference against the existing approved
// setup authority, bound to the plan digest and its exact file scope.
type ApprovalVerifier interface {
	VerifyApproval(ctx context.Context, ref, planDigest string, paths []string) error
}

// Options wires the service.
type Options struct {
	// Environment yields the M3 observed facts (read-only).
	Environment func(context.Context) (protocol.EnvironmentFacts, error)
	// SourceRoots are the protected target-source roots, resolved by the shared
	// facade, never chosen by a host. Strict plans require at least one.
	SourceRoots []string
	// Approvals verifies plan approvals; nil means no approval can be verified.
	Approvals ApprovalVerifier
	// Prober supplies live host/session evidence for Verify; nil means every
	// live check is unavailable.
	Prober Prober
	Clock  clock.Clock
	// CheckPrivateDir overrides the runtime-home protection check (tests).
	CheckPrivateDir func(string) error
}

// Service implements PrincipalHost.
type Service struct{ o Options }

// New builds the service.
func New(o Options) *Service {
	if o.Clock == nil {
		o.Clock = clock.System()
	}
	if o.CheckPrivateDir == nil {
		o.CheckPrivateDir = checkPrivateDir
	}
	return &Service{o: o}
}

// PrincipalHost is the bounded host integration interface.
type PrincipalHost interface {
	Detect(context.Context) ([]HostObservation, error)
	Plan(context.Context, HostObservation, IntegrationRequest) (IntegrationPlan, error)
	ApplyApproved(context.Context, IntegrationPlan, ApprovedPlan) (ApplyReceipt, error)
	Verify(context.Context, IntegrationPlan, string) (VerificationReport, error)
}

var _ PrincipalHost = (*Service)(nil)

// Detect adapts the M3 observation, read-only, sorted by host id and executable.
// Absent hosts are reported as absent, never as an error.
func (s *Service) Detect(ctx context.Context) ([]HostObservation, error) {
	if s.o.Environment == nil {
		return nil, errors.New("principalhosts: no environment source")
	}
	facts, err := s.o.Environment(ctx)
	if err != nil {
		return nil, err
	}
	count := map[string]int{}
	for _, e := range facts.Software {
		if e.Category == protocol.SoftwarePrincipalHost {
			count[e.ID]++
		}
	}
	inv := FromEnvironment(facts)
	at := facts.ObservedAt.Time()
	var out []HostObservation
	for _, h := range inv.Hosts {
		o := HostObservation{
			HostID: string(h.ID), Executable: h.Path, Version: h.Version,
			OS: string(facts.Host.Family), ObservedAt: at,
			EvidenceRefs: []string{"environment:" + string(h.ID)},
		}
		switch {
		case count[string(h.ID)] > 1:
			o.State = StateAmbiguous
		case !h.Installed:
			o.State = StateAbsent
		case h.VersionStatus == protocol.VersionIncompatible:
			o.State = StateIncompatible
		case h.Path == "":
			o.State = StateUnknown
		default:
			o.State = StateDetected
		}
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].HostID != out[j].HostID {
			return out[i].HostID < out[j].HostID
		}
		return out[i].Executable < out[j].Executable
	})
	return out, nil
}

func sha(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func canonicalAbs(name, p string) error {
	if p == "" || !filepath.IsAbs(p) || filepath.Clean(p) != p {
		return fmt.Errorf("%w: %s must be a canonical absolute path", ErrInvalidRequest, name)
	}
	return nil
}

func overlaps(a, b string) bool {
	in := func(root, p string) bool {
		rel, err := filepath.Rel(root, p)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	return in(a, b) || in(b, a)
}

func (s *Service) validate(obs HostObservation, req IntegrationRequest) (string, error) {
	if req.HostID == "" || req.HostID != obs.HostID {
		return "", fmt.Errorf("%w: observation does not match the selected host", ErrHostNotSelected)
	}
	if obs.State != StateDetected {
		return "", fmt.Errorf("%w: host state is %q", ErrHostNotSelected, obs.State)
	}
	if req.HostID == string(HostVSCode) {
		return "", ErrGuidanceOnly
	}
	if req.HostID != string(HostAntigravity) && req.HostID != string(HostCursor) {
		return "", fmt.Errorf("%w: unsupported host %q", ErrInvalidRequest, req.HostID)
	}
	if strings.TrimSpace(req.ProjectID) == "" {
		return "", fmt.Errorf("%w: project id is required", ErrInvalidRequest)
	}
	if req.Mode != ModeStrict && req.Mode != ModeAssisted {
		return "", fmt.Errorf("%w: mode must be strict or assisted", ErrInvalidRequest)
	}
	for name, p := range map[string]string{"principal workspace": req.PrincipalWorkspace, "mcp executable": req.MCPExecutable, "runtime home": req.RuntimeHome} {
		if err := canonicalAbs(name, p); err != nil {
			return "", err
		}
	}
	binding := req.PrincipalBindingPath
	if binding != "" {
		if err := canonicalAbs("principal binding path", binding); err != nil {
			return "", err
		}
	} else {
		binding = filepath.Join(req.RuntimeHome, "config", "principal-binding.json")
	}
	if req.Mode == ModeStrict && len(s.o.SourceRoots) == 0 {
		return "", fmt.Errorf("%w: strict mode needs known target source roots", ErrInvalidRequest)
	}
	for _, root := range s.o.SourceRoots {
		if err := canonicalAbs("source root", root); err != nil {
			return "", err
		}
		if overlaps(root, req.RuntimeHome) || overlaps(root, binding) {
			return "", fmt.Errorf("%w: runtime home or binding overlaps target source", ErrInvalidRequest)
		}
		if req.Mode == ModeStrict && overlaps(root, req.PrincipalWorkspace) {
			return "", fmt.Errorf("%w: strict workspace overlaps target source", ErrInvalidRequest)
		}
	}
	if err := s.o.CheckPrivateDir(req.RuntimeHome); err != nil {
		return "", fmt.Errorf("%w: runtime home: %v", ErrInvalidRequest, err)
	}
	return binding, nil
}

// readPreimage returns the file's bytes, or nil,false when it is absent. A
// symlink or non-regular file is refused.
func readPreimage(path string) ([]byte, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, fmt.Errorf("%w: %s is not a regular file", ErrConfigNotMergeable, path)
	}
	b, err := os.ReadFile(path)
	return b, err == nil, err
}

func preimageHash(b []byte, exists bool) string {
	if !exists {
		return "absent"
	}
	return sha(b)
}

// mergeConfig sets mcpServers.devcadence, preserving every other entry. It
// reports whether an unequal existing owned entry is replaced.
func mergeConfig(existing []byte, exists bool, entry map[string]any, root string) ([]byte, bool, error) {
	doc := map[string]json.RawMessage{}
	if exists {
		if err := json.Unmarshal(existing, &doc); err != nil || doc == nil {
			return nil, false, fmt.Errorf("%w: not a JSON object", ErrConfigNotMergeable)
		}
	}
	servers := map[string]json.RawMessage{}
	if raw, ok := doc[root]; ok {
		if err := json.Unmarshal(raw, &servers); err != nil || servers == nil {
			return nil, false, fmt.Errorf("%w: %q is not an object", ErrConfigNotMergeable, root)
		}
	}
	newEntry, err := json.Marshal(entry)
	if err != nil {
		return nil, false, err
	}
	replaces := false
	if old, ok := servers["devcadence"]; ok {
		var a, b any
		if json.Unmarshal(old, &a) != nil || json.Unmarshal(newEntry, &b) != nil || !jsonEqual(a, b) {
			replaces = true
		}
	}
	servers["devcadence"] = newEntry
	raw, err := json.Marshal(servers)
	if err != nil {
		return nil, false, err
	}
	doc[root] = raw
	var buf bytes.Buffer
	body, err := json.Marshal(doc)
	if err != nil {
		return nil, false, err
	}
	if err := json.Indent(&buf, body, "", "  "); err != nil {
		return nil, false, err
	}
	buf.WriteByte('\n')
	return buf.Bytes(), replaces, nil
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

// ServerEntry renders the devcadence stdio entry shared by every host recipe.
// typed adds the explicit "type":"stdio" (Cursor and VS Code).
func ServerEntry(req IntegrationRequest, typed bool) map[string]any {
	env := map[string]any{"DEVCADENCE_PROJECT_ID": req.ProjectID, "DEVCADENCE_HOME": req.RuntimeHome}
	if req.PrincipalBindingPath != "" {
		env["DEVCADENCE_PRINCIPAL_BINDING"] = req.PrincipalBindingPath
	}
	entry := map[string]any{"command": req.MCPExecutable, "env": env}
	if typed {
		entry["type"] = "stdio"
	}
	return entry
}

// Plan renders the exact files and manual steps. It reads preimages only.
func (s *Service) Plan(ctx context.Context, obs HostObservation, req IntegrationRequest) (IntegrationPlan, error) {
	if err := ctx.Err(); err != nil {
		return IntegrationPlan{}, err
	}
	if _, err := s.validate(obs, req); err != nil {
		return IntegrationPlan{}, err
	}
	ws := req.PrincipalWorkspace
	var cfgPath string
	var typed bool
	var assets [][2]any // path, content
	switch req.HostID {
	case string(HostAntigravity):
		cfgPath = filepath.Join(ws, ".agents", "mcp_config.json")
		assets = [][2]any{
			{filepath.Join(ws, ".agents", "rules", "devcadence-principal.md"), antigravityRule},
			{filepath.Join(ws, ".agents", "skills", "devcadence-principal", "SKILL.md"), antigravitySkill},
		}
	case string(HostCursor):
		cfgPath, typed = filepath.Join(ws, ".cursor", "mcp.json"), true
		assets = [][2]any{{filepath.Join(ws, ".cursor", "rules", "devcadence-principal.mdc"), cursorRule}}
	}
	plan := IntegrationPlan{Request: req, Observation: obs, ApprovalRequired: true}

	existing, exists, err := readPreimage(cfgPath)
	if err != nil {
		return IntegrationPlan{}, err
	}
	merged, replaces, err := mergeConfig(existing, exists, ServerEntry(req, typed), "mcpServers")
	if err != nil {
		return IntegrationPlan{}, err
	}
	plan.Files = append(plan.Files, PlannedFile{Path: cfgPath, BeforeSHA256: preimageHash(existing, exists),
		AfterSHA256: sha(merged), Content: merged, OwnedEntry: "devcadence"})
	if replaces {
		plan.ManualSteps = append(plan.ManualSteps, "REPLACES the existing unequal devcadence entry in "+cfgPath)
	}
	for _, a := range assets {
		p, c := a[0].(string), a[1].([]byte)
		old, ok, err := readPreimage(p)
		if err != nil {
			return IntegrationPlan{}, err
		}
		plan.Files = append(plan.Files, PlannedFile{Path: p, BeforeSHA256: preimageHash(old, ok), AfterSHA256: sha(c), Content: append([]byte(nil), c...)})
	}
	sort.Slice(plan.Files, func(i, j int) bool { return plan.Files[i].Path < plan.Files[j].Path })
	for _, f := range plan.Files {
		plan.ManualSteps = append(plan.ManualSteps, fmt.Sprintf("write the exact planned bytes to %s (sha256 %s, expected previous %s)", f.Path, f.AfterSHA256, f.BeforeSHA256))
	}
	plan.ManualSteps = append(plan.ManualSteps,
		"restart or reload the host session, then run verification against that session; launching, login and installation stay manual")
	plan.Digest = planDigest(plan)
	return plan, nil
}

func planDigest(p IntegrationPlan) string {
	type file struct {
		Path, Before, After, Owned string
		Content                    []byte
	}
	doc := struct {
		Format      string
		Request     IntegrationRequest
		Observation [4]string
		State       string
		Files       []file
		Manual      []string
	}{Format: PlanFormat, Request: p.Request,
		Observation: [4]string{p.Observation.HostID, p.Observation.Executable, p.Observation.Version, p.Observation.OS},
		State:       p.Observation.State, Manual: p.ManualSteps}
	for _, f := range p.Files {
		doc.Files = append(doc.Files, file{f.Path, f.BeforeSHA256, f.AfterSHA256, f.OwnedEntry, f.Content})
	}
	sort.Slice(doc.Files, func(i, j int) bool { return doc.Files[i].Path < doc.Files[j].Path })
	b, _ := json.Marshal(doc)
	return sha(b)
}

// ApplyApproved never writes. No enforced exclusive operator-owned mutation
// channel exists in this build, and M3 setup has no operation kind for host
// configuration, so the approved plan is applied by the operator. It still
// refuses a tampered plan, a wrong or unverifiable approval and a stale
// preimage, so the operator is never handed a stale plan.
func (s *Service) ApplyApproved(ctx context.Context, plan IntegrationPlan, ap ApprovedPlan) (ApplyReceipt, error) {
	receipt := ApplyReceipt{PlanDigest: plan.Digest, ApprovalRef: ap.ApprovalRef}
	if plan.Digest == "" || planDigest(plan) != plan.Digest || ap.PlanDigest != plan.Digest || strings.TrimSpace(ap.ApprovalRef) == "" {
		return receipt, ErrUnauthorized
	}
	if s.o.Approvals == nil {
		receipt.Manual = true
		receipt.EvidenceRefs = []string{"manual-required:approval-authority-unavailable"}
		return receipt, nil
	}
	paths := make([]string, 0, len(plan.Files))
	for _, f := range plan.Files {
		paths = append(paths, f.Path)
	}
	if err := s.o.Approvals.VerifyApproval(ctx, ap.ApprovalRef, plan.Digest, paths); err != nil {
		return receipt, fmt.Errorf("%w: %v", ErrUnauthorized, err)
	}
	for _, f := range plan.Files {
		b, ok, err := readPreimage(f.Path)
		if err != nil || preimageHash(b, ok) != f.BeforeSHA256 {
			if sha(b) == f.AfterSHA256 && ok {
				continue // already applied by the operator
			}
			receipt.FailedPaths = append(receipt.FailedPaths, f.Path)
		}
	}
	if len(receipt.FailedPaths) > 0 {
		return receipt, ErrStalePreimage
	}
	receipt.Manual = true
	receipt.EvidenceRefs = []string{"manual-required:no-exclusive-mutation-channel"}
	return receipt, nil
}
