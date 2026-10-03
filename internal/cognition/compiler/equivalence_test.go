package compiler_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/olostan/DevCadence/internal/cognition/compiler"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

const (
	goldenNormativeSourceDigest     = "sha256:ae94b28606098e0ba8e9b50fbb11f2b9d005fda25021e651164203831773a9d0"
	goldenAuthorityProjectionDigest = "sha256:1f7b8233d67a37a161e4731d9f0402f19903dc6d600f3a9e4cfaf235906d71da"
	goldenCatalogDigest             = "sha256:f33655bd50a8e82afa6fd3c1fd823b9d7ad6a88e79be38fcb902e01bc40caab5"
	goldenPackDigest                = "sha256:beb6cb6d93b1b900fd7e12ab4c005fe36886ee7ed9041f7216e64c63fc3b7d9c"
	goldenInvocationDigestMarkdown  = "sha256:ffe1b46773d465841f32174b41b34583521bbdb346961caebe06a3b3ad26d3a0"
	goldenInvocationDigestJSON      = "sha256:4b8b08091cf59c69c66e36101dc13bc91b4e5ed986f43db5fee68652685bd93c"
)

func TestEquivalence_CanonicalCatalogDigests(t *testing.T) {
	reg, err := compiler.NewCanonicalRuleRegistry()
	if err != nil {
		t.Fatalf("failed to create canonical rule registry: %v", err)
	}

	if reg.NormativeSourceDigest() != goldenNormativeSourceDigest {
		t.Errorf("NormativeSourceDigest mismatch:\ngot:  %s\nwant: %s", reg.NormativeSourceDigest(), goldenNormativeSourceDigest)
	}
	if reg.AuthorityProjectionDigest() != goldenAuthorityProjectionDigest {
		t.Errorf("AuthorityProjectionDigest mismatch:\ngot:  %s\nwant: %s", reg.AuthorityProjectionDigest(), goldenAuthorityProjectionDigest)
	}
	if reg.CatalogDigest() != goldenCatalogDigest {
		t.Errorf("CatalogDigest mismatch:\ngot:  %s\nwant: %s", reg.CatalogDigest(), goldenCatalogDigest)
	}
}

func TestEquivalence_CompiledPackAndInvocationDigests(t *testing.T) {
	reg, err := compiler.NewCanonicalRuleRegistry()
	if err != nil {
		t.Fatalf("failed to create canonical rule registry: %v", err)
	}

	leaseMgr := compiler.NewEvidenceLeaseManager()
	capsuleMgr := compiler.NewCapsuleManager()
	c, err := compiler.NewCompiler(reg, leaseMgr, capsuleMgr)
	if err != nil {
		t.Fatalf("NewCompiler failed: %v", err)
	}

	profile := compiler.MustDefaultProvisionalProfile("ep_1", "chan_1", "qwen2.5-coder", 32768)
	req := compiler.CompileRequest{
		TaskID:               "task-equivalence-001",
		WorkPackageID:        "WP-M3C-TEST",
		WorkPackageRevision:  1,
		WorkPackageDigest:    "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		Role:                 "implementer",
		BaseCommit:           compiler.CanonicalCatalogRevision,
		SourceRevision:       compiler.CanonicalCatalogRevision,
		ProjectStateRevision: "rev-bootstrap-001",
		BudgetPoolID:         "default_pool",
		ReadEnvelope:         []string{"internal/*"},
		WriteScope:           []string{"internal/cognition/*"},
		Domains:              []string{"cognition"},
		Action:               "Implement test package",
		ActiveCapabilities:   []string{"write"},
		ExecutionContract:    "Execute deterministic compilation without data loss.",
		ContextProfile:       profile,
		Assumptions: []protocol.Assumption{
			{
				ID:        "asm_1",
				Statement: "Base commit matches main",
				Status:    protocol.AssumptionVerified,
				Material:  true,
			},
		},
		ExplicitQuestions: []string{"Does pack compile cleanly?"},
		Renderer:          compiler.NewTaggedMarkdownRenderer(),
	}

	// 1. Tagged markdown projection
	invMD, err := c.CompileInvocation(context.Background(), req)
	if err != nil {
		t.Fatalf("CompileInvocation (Markdown) failed: %v", err)
	}

	if invMD.Pack.PackDigest != goldenPackDigest {
		t.Errorf("PackDigest mismatch:\ngot:  %s\nwant: %s", invMD.Pack.PackDigest, goldenPackDigest)
	}
	if invMD.InvocationDigest != goldenInvocationDigestMarkdown {
		t.Errorf("InvocationDigest (Markdown) mismatch:\ngot:  %s\nwant: %s", invMD.InvocationDigest, goldenInvocationDigestMarkdown)
	}

	// Verify exact admitted rule IDs and deterministic order
	var admittedIDs []string
	for _, mc := range invMD.Manifest.MandatoryClauses {
		admittedIDs = append(admittedIDs, mc.ClauseID)
	}
	expectedIDs := []string{
		"DCI-001", "DCI-002", "DCI-003", "DCI-004", "DCI-005", "DCI-006", "DCI-007",
		"DCI-010", "DCI-011", "DCI-012", "DCI-013", "DCI-018", "DCI-019", "DCI-024",
		"DCI-025", "DCI-030", "DCI-031", "DCI-034", "DCI-055", "DCI-084", "DCI-091",
		"DCI-104", "DCI-120", "DCI-121", "DCI-122", "DCI-123", "DCI-124", "DCI-125",
		"DCI-126", "DCI-127", "DCI-128", "DCI-129", "DCI-130", "DCI-131",
	}
	if !reflect.DeepEqual(admittedIDs, expectedIDs) {
		t.Errorf("Admitted IDs mismatch:\ngot:  %v\nwant: %v", admittedIDs, expectedIDs)
	}

	// 2. JSON projection (PackDigest must match goldenPackDigest, InvocationDigest matches goldenInvocationDigestJSON)
	reqJSON := req
	reqJSON.Renderer = compiler.NewJSONRenderer()
	invJSON, err := c.CompileInvocation(context.Background(), reqJSON)
	if err != nil {
		t.Fatalf("CompileInvocation (JSON) failed: %v", err)
	}

	if invJSON.Pack.PackDigest != goldenPackDigest {
		t.Errorf("PackDigest (JSON) mismatch:\ngot:  %s\nwant: %s", invJSON.Pack.PackDigest, goldenPackDigest)
	}
	if invJSON.InvocationDigest != goldenInvocationDigestJSON {
		t.Errorf("InvocationDigest (JSON) mismatch:\ngot:  %s\nwant: %s", invJSON.InvocationDigest, goldenInvocationDigestJSON)
	}
}

func TestEquivalence_ToolAuthorityAndBindingGuarantees(t *testing.T) {
	reg, err := compiler.NewCanonicalRuleRegistry()
	if err != nil {
		t.Fatalf("failed to create canonical rule registry: %v", err)
	}

	c, err := compiler.NewCompiler(reg, nil, nil)
	if err != nil {
		t.Fatalf("NewCompiler failed: %v", err)
	}
	profile := compiler.MustDefaultProvisionalProfile("ep_1", "chan_1", "model", 32768)

	baseReq := compiler.CompileRequest{
		TaskID:               "task-tool-001",
		WorkPackageID:        "WP-M3C-TEST",
		WorkPackageRevision:  1,
		WorkPackageDigest:    "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		Role:                 "implementer",
		BaseCommit:           compiler.CanonicalCatalogRevision,
		SourceRevision:       compiler.CanonicalCatalogRevision,
		ProjectStateRevision: "rev-bootstrap-001",
		BudgetPoolID:         "default_pool",
		ExecutionContract:    "Contract",
		ContextProfile:       profile,
	}

	t.Run("modern and legacy tools are mutually exclusive", func(t *testing.T) {
		req := baseReq
		req.ToolSchemas = []string{`{"name": "fetch"}`}
		req.Tools = []string{"fetch"}
		req.DeclaredTools = []compiler.ToolCapabilityInfo{{Name: "fetch", ReadOnly: true}}
		_, err := c.CompileInvocation(context.Background(), req)
		if !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument for mixing ToolSchemas and Tools, got %v", err)
		}
	})

	t.Run("missing tool capability declaration fails closed", func(t *testing.T) {
		req := baseReq
		req.ToolSchemas = []string{`{"name": "fetch"}`}
		_, err := c.CompileInvocation(context.Background(), req)
		if !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument when tool schema lacks declaration, got %v", err)
		}
	})

	t.Run("empty tool capability without read-only fails closed", func(t *testing.T) {
		tool := compiler.ToolCapabilityInfo{Name: "mystery_tool"}
		if err := tool.Validate(); !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument for empty capability tool without ReadOnly, got %v", err)
		}
	})

	t.Run("alias normalization implies canonical capability", func(t *testing.T) {
		aliases := []struct {
			input     string
			canonical string
		}{
			{"repository_mutation", "write"},
			{"write", "write"},
			{"process_execution", "exec"},
			{"exec", "exec"},
			{"network_access", "network"},
			{"network", "network"},
			{"credentials", "credentials"},
			{"spending", "spending"},
			{"durable_state_mutation", "durable_state_mutation"},
			{"read_only", "read_only"},
		}
		for _, tc := range aliases {
			canon, err := compiler.NormalizeCapability(tc.input)
			if err != nil {
				t.Errorf("NormalizeCapability(%q) returned error: %v", tc.input, err)
			}
			if canon != tc.canonical {
				t.Errorf("NormalizeCapability(%q) = %q, want %q", tc.input, canon, tc.canonical)
			}
		}
	})
}
