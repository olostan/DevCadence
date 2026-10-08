package empirical

import (
	"context"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/olostan/DevCadence/internal/clock"
)

func TestNewAdmitterValidation(t *testing.T) {
	auth := allowAuthority{}
	ver := &countingVerifier{}
	store := memStore{}
	clk := clock.NewFake(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), 0)

	if _, err := NewAdmitter(AdmitterOptions{Verifier: ver, Resolver: store, Clock: clk}); err == nil {
		t.Fatal("expected error with nil authority")
	}
	if _, err := NewAdmitter(AdmitterOptions{Authority: auth, Resolver: store, Clock: clk}); err == nil {
		t.Fatal("expected error with nil verifier")
	}
	if _, err := NewAdmitter(AdmitterOptions{Authority: auth, Verifier: ver, Clock: clk}); err == nil {
		t.Fatal("expected error with nil resolver")
	}
	if _, err := NewAdmitter(AdmitterOptions{Authority: auth, Verifier: ver, Resolver: store}); err == nil {
		t.Fatal("expected error with nil clock")
	}

	adm, err := NewAdmitter(AdmitterOptions{
		Authority: auth,
		Verifier:  ver,
		Resolver:  store,
		Clock:     clk,
	})
	if err != nil || adm == nil {
		t.Fatalf("expected NewAdmitter success, got err: %v", err)
	}
}

func TestAdmitterAdmitAuthorityWindow(t *testing.T) {
	cases := map[string]struct {
		auth allowAuthority
		code string
	}{
		"zero issued at": {
			auth: allowAuthority{
				custom: true,
				window: AuthorityWindow{
					IssuedAt: time.Time{},
					NotAfter: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
				},
			},
			code: ReasonOperatorAuthority,
		},
		"zero not after": {
			auth: allowAuthority{
				custom: true,
				window: AuthorityWindow{
					IssuedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
					NotAfter: time.Time{},
				},
			},
			code: ReasonOperatorAuthority,
		},
		"not after before issued at": {
			auth: allowAuthority{
				custom: true,
				window: AuthorityWindow{
					IssuedAt: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
					NotAfter: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
				},
			},
			code: ReasonOperatorAuthority,
		},
		"authority verify error": {
			auth: allowAuthority{err: errors.New("signature expired")},
			code: ReasonOperatorAuthority,
		},
		"session started before window": {
			auth: allowAuthority{
				custom: true,
				window: AuthorityWindow{
					IssuedAt: time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC), // after session started at 10:00
					NotAfter: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
				},
			},
			code: ReasonAuthorizationBad,
		},
		"session started after window": {
			auth: allowAuthority{
				custom: true,
				window: AuthorityWindow{
					IssuedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
					NotAfter: time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC), // before session started at 10:00
				},
			},
			code: ReasonAuthorizationBad,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			m := f.manifest()
			v := &countingVerifier{}
			adm, err := NewAdmitter(AdmitterOptions{
				Authority: tc.auth,
				Verifier:  v,
				Resolver:  f.store,
				Clock:     testClock,
			})
			if err != nil {
				t.Fatal(err)
			}
			res, err := adm.Admit(context.Background(), m)
			if err != nil {
				t.Fatal(err)
			}
			if res.Admitted || !hasCode(res, tc.code) {
				t.Fatalf("admitted=%v, codes=%v, want code %s", res.Admitted, res.ReasonCodes, tc.code)
			}
		})
	}
}

func TestAdmitterAdmitFutureTimestamp(t *testing.T) {
	f := newFixture()
	m := f.manifest()
	v := &countingVerifier{}
	// Session started at 10:00:00; clock is at 09:00:00
	pastClock := clock.NewFake(time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC), 0)
	adm, err := NewAdmitter(AdmitterOptions{
		Authority: allowAuthority{},
		Verifier:  v,
		Resolver:  f.store,
		Clock:     pastClock,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := adm.Admit(context.Background(), m)
	if err != nil {
		t.Fatal(err)
	}
	if res.Admitted || !hasCode(res, ReasonRunInvalid) {
		t.Fatalf("admitted=%v, codes=%v, want %s", res.Admitted, res.ReasonCodes, ReasonRunInvalid)
	}
}

func setupMeteredFixture(planMicroUSD, authMicroUSD int64, spendPerRunUSD float64) (*fixture, CampaignManifest) {
	f := newFixture()
	for i := range f.plan.Runs {
		f.plan.Runs[i].Endpoint.CapabilityClass = "frontier_api"
		f.runs[i].Endpoint.CapabilityClass = "frontier_api"
		id := f.runs[i].RunID
		if snap, ok := f.snaps[id]; ok {
			snap.Capability = "frontier_api"
			f.snaps[id] = snap
		}
		val := spendPerRunUSD
		f.runs[i].Measurements["api_spend_usd"] = Measurement{
			Known: true, Value: &val, Unit: "USD", Provenance: ProvenanceMeasured, EvidenceRef: "ev/api_spend",
		}
	}
	f.plan.RequestedTiers = []string{"frontier_api"}
	f.plan.Limits.AllowMetered = true
	f.plan.Limits.MaxAPISpendMicroUSD = planMicroUSD
	f.auth.AllowedEndpointBindings[0].CapabilityClass = "frontier_api"
	f.auth.AllowMetered = true
	f.auth.MaxAPISpendMicroUSD = authMicroUSD
	m := f.manifest()
	return f, m
}

func TestAdmitterSpendCapEnforcement(t *testing.T) {
	t.Run("spend within authorized cap admitted", func(t *testing.T) {
		// 20 runs x $0.50 ($500,000 micro-USD) = 10,000,000 micro-USD total spend
		f, m := setupMeteredFixture(10000000, 10000000, 0.50)
		v := &countingVerifier{}
		adm, err := NewAdmitter(AdmitterOptions{
			Authority: allowAuthority{},
			Verifier:  v,
			Resolver:  f.store,
			Clock:     testClock,
		})
		if err != nil {
			t.Fatal(err)
		}
		res, err := adm.Admit(context.Background(), m)
		if err != nil {
			t.Fatal(err)
		}
		if !res.Admitted {
			t.Fatalf("expected admitted, got codes=%v limitations=%v", res.ReasonCodes, res.Limitations)
		}
	})

	t.Run("spend exceeding authorized cap denied", func(t *testing.T) {
		// 20 runs x $0.50 = 10,000,000 micro-USD, authorized cap is 9,999,999, plan cap is 20,000,000
		f, m := setupMeteredFixture(20000000, 9999999, 0.50)
		v := &countingVerifier{}
		adm, err := NewAdmitter(AdmitterOptions{
			Authority: allowAuthority{},
			Verifier:  v,
			Resolver:  f.store,
			Clock:     testClock,
		})
		if err != nil {
			t.Fatal(err)
		}
		res, err := adm.Admit(context.Background(), m)
		if err != nil {
			t.Fatal(err)
		}
		if res.Admitted || !hasCode(res, ReasonAuthorizationBad) {
			t.Fatalf("admitted=%v, codes=%v, want %s", res.Admitted, res.ReasonCodes, ReasonAuthorizationBad)
		}
	})
}

func TestAdmitterVerifierCommitmentChecks(t *testing.T) {
	cases := map[string]struct {
		mut  func(o *VerifiedOutcome, run RunEvidence)
		code string
	}{
		"verifier source commit mismatch": {
			mut: func(o *VerifiedOutcome, _ RunEvidence) {
				o.VerifierSourceCommit = "different-commit"
			},
			code: ReasonVerifierRejected,
		},
		"verification profile digest mismatch": {
			mut: func(o *VerifiedOutcome, _ RunEvidence) {
				o.VerificationProfileDigest = dg("different-profile")
			},
			code: ReasonVerifierRejected,
		},
		"session digest mismatch": {
			mut: func(o *VerifiedOutcome, _ RunEvidence) {
				o.SessionDigest = dg("different-session")
			},
			code: ReasonVerifierRejected,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture()
			m := f.manifest()
			v := &countingVerifier{mut: tc.mut}
			adm, err := NewAdmitter(AdmitterOptions{
				Authority: allowAuthority{},
				Verifier:  v,
				Resolver:  f.store,
				Clock:     testClock,
			})
			if err != nil {
				t.Fatal(err)
			}
			res, err := adm.Admit(context.Background(), m)
			if err != nil {
				t.Fatal(err)
			}
			if res.Admitted || !hasCode(res, tc.code) {
				t.Fatalf("admitted=%v, codes=%v, want %s", res.Admitted, res.ReasonCodes, tc.code)
			}
		})
	}
}

func TestPackageIsolation(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("ParseDir failed: %v", err)
	}

	prohibited := []string{
		"internal/benchmark/receipts",
		"internal/benchmark/actors",
		"internal/benchmark/sessionclients",
		"internal/benchmark/verifier",
	}

	for _, pkg := range pkgs {
		for filename, file := range pkg.Files {
			for _, imp := range file.Imports {
				path := strings.Trim(imp.Path.Value, `"`)
				for _, p := range prohibited {
					if strings.Contains(path, p) {
						t.Fatalf("file %s imports prohibited package %q (%s)", filepath.Base(filename), p, path)
					}
				}
			}
		}
	}
}
