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

func TestEquivalence_CanonicalCatalogDigests(t *testing.T) {
	reg, err := compiler.NewCanonicalRuleRegistry()
	if err != nil {
		t.Fatalf("failed to create canonical rule registry: %v", err)
	}

	checkGolden(t, goldenNormativeSource, reg.NormativeSourceDigest())
	checkGolden(t, goldenAuthorityProjection, reg.AuthorityProjectionDigest())
	checkGolden(t, goldenCatalog, reg.CatalogDigest())
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

	checkGolden(t, goldenPack, invMD.Pack.PackDigest)
	checkGolden(t, goldenInvocationMarkdown, invMD.InvocationDigest)

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

	// 2. JSON projection (same PackDigest, its own InvocationDigest)
	reqJSON := req
	reqJSON.Renderer = compiler.NewJSONRenderer()
	invJSON, err := c.CompileInvocation(context.Background(), reqJSON)
	if err != nil {
		t.Fatalf("CompileInvocation (JSON) failed: %v", err)
	}

	checkGolden(t, goldenPack, invJSON.Pack.PackDigest)
	checkGolden(t, goldenInvocationJSON, invJSON.InvocationDigest)
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
