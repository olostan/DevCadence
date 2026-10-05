package principalhosts

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// Probe results. For an access route, ResultPass means the access was DENIED
// (the desired strict outcome); ResultFail means it succeeded.
const (
	ResultPass        = "pass"
	ResultFail        = "fail"
	ResultUnavailable = "unavailable"
)

// RequiredRoutes are the native source-access routes a strict verification must
// either deny or show the host disables. Any other enabled route is tested too.
var RequiredRoutes = []string{
	"native_read_absolute", "native_search_index", "native_write",
	"terminal_read", "terminal_write", "shell_escape",
}

// ProbeCheck is one captured check. ObservedAt is stamped by Verify, never by
// the prober, so evidence is fresh by construction.
type ProbeCheck struct {
	Route, Result, EvidenceRef string
	ObservedAt                 time.Time
}

// VerificationReport is valid only for the observed session and identities.
type VerificationReport struct {
	HostID, HostVersion, OS, SessionRef, PlanDigest string
	State                                           string
	Checks                                          []ProbeCheck
	Reasons                                         []string
	// Identities bind the report: file digests, executable, host version, OS,
	// policy identity and route inventory.
	Identities map[string]string
}

// SessionInfo is what the live host session reports about itself.
type SessionInfo struct {
	HostVersion, OS string
	// PolicyIdentity identifies the host permission policy; empty means policy
	// introspection is unavailable.
	PolicyIdentity string
	// EnabledRoutes is the route inventory; nil means unavailable.
	EnabledRoutes []string
	// DisabledRoutes maps a route to evidence the installed host disables it.
	DisabledRoutes map[string]string
}

// Prober supplies live host-session evidence. A real implementation needs an
// installed host and a human-operated session; none ships in this build.
type Prober interface {
	Session(ctx context.Context, sessionRef string) (SessionInfo, error)
	Connect(ctx context.Context, plan IntegrationPlan) ProbeCheck
	Instructions(ctx context.Context, plan IntegrationPlan, sessionRef string) ProbeCheck
	// Access tries the harmless externally prepared sentinel through one route
	// and reports only success or denial, never contents.
	Access(ctx context.Context, sessionRef, route string) ProbeCheck
}

func (s *Service) stamp(ctx context.Context, c ProbeCheck) ProbeCheck {
	if c.Result != ResultPass && c.Result != ResultFail {
		c.Result = ResultUnavailable
	}
	if ctx.Err() != nil && c.Result == ResultPass {
		c.Result = ResultUnavailable
	}
	c.ObservedAt = s.o.Clock.Now()
	return c
}

func (s *Service) identities(plan IntegrationPlan, info SessionInfo) (map[string]string, []string) {
	id := map[string]string{"host_version": info.HostVersion, "os": info.OS, "policy": info.PolicyIdentity}
	var reasons []string
	routes := append([]string(nil), info.EnabledRoutes...)
	sort.Strings(routes)
	id["routes"] = strings.Join(routes, ",")
	for _, f := range plan.Files {
		b, ok, err := readPreimage(f.Path)
		switch {
		case err != nil || !ok:
			id["file:"+f.Path] = "absent"
			reasons = append(reasons, "planned file missing or unreadable: "+f.Path)
		default:
			id["file:"+f.Path] = sha(b)
			if sha(b) != f.AfterSHA256 {
				reasons = append(reasons, "configuration drift from the approved plan: "+f.Path)
			}
		}
	}
	if b, err := os.ReadFile(plan.Request.MCPExecutable); err != nil {
		id["executable"] = "unreadable"
		reasons = append(reasons, "executable unreadable")
	} else {
		id["executable"] = sha(b)
	}
	return id, reasons
}

// Verify builds a fresh report. It mutates nothing and never reuses a prior
// report. Any unavailable, timed-out or cancelled check is never a pass.
func (s *Service) Verify(ctx context.Context, plan IntegrationPlan, sessionRef string) (VerificationReport, error) {
	rep := VerificationReport{HostID: plan.Request.HostID, SessionRef: sessionRef, PlanDigest: plan.Digest, State: Unverified}
	if plan.Digest == "" || planDigest(plan) != plan.Digest {
		return rep, ErrUnauthorized
	}
	if strings.TrimSpace(sessionRef) == "" || s.o.Prober == nil {
		rep.Reasons = append(rep.Reasons, "no live host session evidence is available")
		return rep, nil
	}
	info, err := s.o.Prober.Session(ctx, sessionRef)
	if err != nil || info.HostVersion == "" || info.OS == "" {
		rep.Reasons = append(rep.Reasons, "host session identity unavailable")
		return rep, nil
	}
	rep.HostVersion, rep.OS = info.HostVersion, info.OS
	var drift []string
	rep.Identities, drift = s.identities(plan, info)
	if len(drift) > 0 {
		rep.State, rep.Reasons = Blocked, drift
		return rep, nil
	}

	conn := s.stamp(ctx, s.o.Prober.Connect(ctx, plan))
	conn.Route = "mcp_connection"
	instr := s.stamp(ctx, s.o.Prober.Instructions(ctx, plan, sessionRef))
	instr.Route = "instructions"
	rep.Checks = append(rep.Checks, conn, instr)
	state := Unverified
	for _, c := range []ProbeCheck{conn, instr} {
		switch c.Result {
		case ResultFail:
			state = Blocked
			rep.Reasons = append(rep.Reasons, c.Route+" failed")
		case ResultUnavailable:
			if state != Blocked {
				state = Unverified
			}
			rep.Reasons = append(rep.Reasons, c.Route+" unavailable")
		}
	}
	basic := conn.Result == ResultPass && instr.Result == ResultPass

	isolated := s.verifyIsolation(ctx, &rep, info, sessionRef)
	switch {
	case !basic:
		rep.State = state
	case plan.Request.Mode == ModeStrict && isolated:
		rep.State = ReadyStrict
	case plan.Request.Mode == ModeStrict:
		rep.State = Blocked
	default:
		rep.State = ReadyAssisted
	}
	return rep, nil
}

// verifyIsolation runs every enabled route and checks required-route coverage.
// It reports whether complete denial evidence exists, recording each shortfall.
func (s *Service) verifyIsolation(ctx context.Context, rep *VerificationReport, info SessionInfo, ref string) bool {
	if info.PolicyIdentity == "" {
		rep.Reasons = append(rep.Reasons, "isolation: policy introspection unavailable")
		return false
	}
	if info.EnabledRoutes == nil {
		rep.Reasons = append(rep.Reasons, "isolation: route inventory unavailable")
		return false
	}
	ok := true
	enabled := map[string]bool{}
	for _, r := range info.EnabledRoutes {
		enabled[r] = true
	}
	for _, r := range RequiredRoutes {
		if !enabled[r] && info.DisabledRoutes[r] == "" {
			rep.Reasons = append(rep.Reasons, "isolation: required route neither tested nor shown disabled: "+r)
			ok = false
		}
	}
	routes := append([]string(nil), info.EnabledRoutes...)
	sort.Strings(routes)
	for _, r := range routes {
		c := s.stamp(ctx, s.o.Prober.Access(ctx, ref, r))
		c.Route = r
		rep.Checks = append(rep.Checks, c)
		if c.Result != ResultPass {
			ok = false
			rep.Reasons = append(rep.Reasons, fmt.Sprintf("isolation: route %s result %s", r, c.Result))
		}
	}
	return ok
}

// Reuse returns nil only when the stored report's session and every bound
// identity are unchanged; otherwise reverification is required. A report is
// evidence, never a cached ready flag.
func (s *Service) Reuse(ctx context.Context, rep VerificationReport, plan IntegrationPlan, sessionRef string) error {
	if s.o.Prober == nil || rep.PlanDigest != plan.Digest || rep.SessionRef != sessionRef || planDigest(plan) != plan.Digest ||
		(rep.State != ReadyStrict && rep.State != ReadyAssisted) {
		return ErrReverification
	}
	info, err := s.o.Prober.Session(ctx, sessionRef)
	if err != nil {
		return ErrReverification
	}
	now, drift := s.identities(plan, info)
	if len(drift) > 0 || len(now) != len(rep.Identities) {
		return ErrReverification
	}
	for k, v := range now {
		if rep.Identities[k] != v {
			return ErrReverification
		}
	}
	return nil
}
