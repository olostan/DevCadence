package compiler_test

import (
	"context"
	"testing"

	"github.com/olostan/DevCadence/internal/cognition/compiler"
)

func TestCompiler_InvocationDigestVsPackDigest(t *testing.T) {
	reg := setupTestRegistry(t)
	c := mustNewCompiler(t, reg, nil, nil)
	profile := compiler.MustDefaultProvisionalProfile("ep_1", "chan_1", "model", 32768)

	req1 := validCompileRequest(profile)
	req1.Renderer = compiler.NewTaggedMarkdownRenderer()

	req2 := validCompileRequest(profile)
	req2.Renderer = compiler.NewJSONRenderer()

	_, pack1, err1 := c.Compile(context.Background(), req1)
	if err1 != nil {
		t.Fatalf("compile 1 failed: %v", err1)
	}

	_, pack2, err2 := c.Compile(context.Background(), req2)
	if err2 != nil {
		t.Fatalf("compile 2 failed: %v", err2)
	}

	// Semantic PackDigest must be identical (same semantic content)
	if pack1.PackDigest != pack2.PackDigest {
		t.Errorf("expected identical PackDigest across renderers, got %q vs %q", pack1.PackDigest, pack2.PackDigest)
	}

	// True InvocationDigest must differ (different endpoint projection)
	if pack1.InvocationDigest == pack2.InvocationDigest {
		t.Errorf("expected different InvocationDigest across renderers, got identical %q", pack1.InvocationDigest)
	}

	// Changing tool schemas must alter InvocationDigest while preserving PackDigest
	req3 := validCompileRequest(profile)
	req3.Renderer = compiler.NewTaggedMarkdownRenderer()
	req3.ToolSchemas = []string{`{"type": "function", "name": "do_task"}`}
	req3.DeclaredTools = []compiler.ToolCapabilityInfo{{Name: "do_task", ReadOnly: true}}

	_, pack3, err3 := c.Compile(context.Background(), req3)
	if err3 != nil {
		t.Fatalf("compile 3 failed: %v", err3)
	}

	if pack1.PackDigest != pack3.PackDigest {
		t.Errorf("expected identical PackDigest when only tool schemas differ, got %q vs %q", pack1.PackDigest, pack3.PackDigest)
	}
	if pack1.InvocationDigest == pack3.InvocationDigest {
		t.Errorf("expected different InvocationDigest when tool schemas differ, got identical %q", pack1.InvocationDigest)
	}
}

func TestCompiler_JSONRenderer_InvocationDigestSelfConsistency(t *testing.T) {
	// Re-render returned pack with JSON renderer -> assert actual invocation projection == projection measured and hashed
	reg := setupTestRegistry(t)
	c := mustNewCompiler(t, reg, nil, nil)
	profile := compiler.MustDefaultProvisionalProfile("ep_1", "chan_1", "model", 32768)

	req := validCompileRequest(profile)
	jsonRenderer := compiler.NewJSONRenderer()
	req.Renderer = jsonRenderer

	inv, err := c.CompileInvocation(context.Background(), req)
	if err != nil {
		t.Fatalf("compile invocation failed: %v", err)
	}

	measuredProj := inv.Projection

	// Now re-render the returned pack (which has pack.InvocationDigest set)
	reRenderedProj, err := jsonRenderer.Render(inv.Pack)
	if err != nil {
		t.Fatalf("re-render failed: %v", err)
	}

	if reRenderedProj.UserPrompt != measuredProj.UserPrompt {
		t.Errorf("re-rendered UserPrompt differs from measured prompt projection!\nMeasured:\n%s\nRe-rendered:\n%s",
			measuredProj.UserPrompt, reRenderedProj.UserPrompt)
	}
	if reRenderedProj.Digest != measuredProj.Digest {
		t.Errorf("re-rendered Digest %q differs from measured Digest %q",
			reRenderedProj.Digest, measuredProj.Digest)
	}
}

func TestInvocationDigest_IncludesMappingRevision(t *testing.T) {
	packDigest := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	profile := compiler.MustDefaultProvisionalProfile("ep_1", "chan_1", "model", 32768)

	d1, err := compiler.ComputeInvocationDigest(
		packDigest, "json", "sys", "user", []string{`{"name":"tool"}`}, "framing",
		"cat-1", "rev-1", "map-rev-1", "sha256:catdigest1", profile,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	d2, err := compiler.ComputeInvocationDigest(
		packDigest, "json", "sys", "user", []string{`{"name":"tool"}`}, "framing",
		"cat-1", "rev-1", "map-rev-2", "sha256:catdigest1", profile,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if d1 == d2 {
		t.Errorf("expected InvocationDigest to change when MappingRevision changes, got identical %q", d1)
	}
}
