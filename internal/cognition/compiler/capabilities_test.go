package compiler_test

import (
	"context"
	"errors"
	"testing"

	"github.com/olostan/DevCadence/internal/cognition/compiler"
	"github.com/olostan/DevCadence/internal/errs"
)

func TestToolCapabilityInfo_FailClosedValidation(t *testing.T) {
	// Empty capabilities and ReadOnly == false fails closed
	t1 := compiler.ToolCapabilityInfo{Name: "tool1"}
	if err := t1.Validate(); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for empty capabilities with ReadOnly == false, got %v", err)
	}

	// ReadOnly: true without capabilities is valid
	t2 := compiler.ToolCapabilityInfo{Name: "tool2", ReadOnly: true}
	if err := t2.Validate(); err != nil {
		t.Errorf("expected valid for ReadOnly: true tool, got %v", err)
	}

	// ReadOnly: true with MutatesFiles: true fails closed
	t3 := compiler.ToolCapabilityInfo{Name: "tool3", ReadOnly: true, MutatesFiles: true}
	if err := t3.Validate(); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for ReadOnly: true + MutatesFiles: true, got %v", err)
	}

	// ReadOnly: true with privileged capability fails closed
	t4 := compiler.ToolCapabilityInfo{
		Name:                 "tool4",
		ReadOnly:             true,
		RequiredCapabilities: []compiler.CapabilityClass{compiler.CapabilityClassWrite},
	}
	if err := t4.Validate(); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for ReadOnly: true + Write capability, got %v", err)
	}
}

func TestCompiler_TypedCapabilitiesDerivation(t *testing.T) {
	// 1. Tool declaration with empty capabilities and ReadOnly == false MUST fail closed
	toolsNoMeta := []compiler.ToolCapabilityInfo{
		{Name: "bash"},
		{Name: "edit_file"},
	}
	_, err := compiler.DeriveActiveCapabilities(nil, toolsNoMeta, nil)
	if err == nil || !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for tools with empty capabilities and ReadOnly == false, got %v", err)
	}

	// ReadOnly tools without privileged capabilities succeed and yield 'read_only'
	toolsReadOnly := []compiler.ToolCapabilityInfo{
		{Name: "fetch_info", ReadOnly: true},
	}
	capsRO, err := compiler.DeriveActiveCapabilities(nil, toolsReadOnly, nil)
	if err != nil {
		t.Fatalf("unexpected error deriving capabilities for read-only tool: %v", err)
	}
	if len(capsRO) != 1 || capsRO[0] != "read_only" {
		t.Errorf("expected ['read_only'], got %v", capsRO)
	}

	// 2. Typed annotations grant exact capabilities and normalize aliases
	toolsTyped := []compiler.ToolCapabilityInfo{
		{Name: "bash", RequiredCapabilities: []compiler.CapabilityClass{compiler.CapabilityClassExec}},
		{Name: "editor", MutatesFiles: true},
	}
	caps, err := compiler.DeriveActiveCapabilities(nil, toolsTyped, []string{"network"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := map[string]bool{"exec": true, "write": true, "filesystem": true, "network": true}
	for _, c := range caps {
		if !expected[c] {
			t.Errorf("unexpected capability derived: %q", c)
		}
	}
	if len(caps) != 4 {
		t.Errorf("expected 4 capabilities (exec, write, filesystem, network), got %v", caps)
	}

	// 3. Unrecognized capability string fails closed
	_, err = compiler.DeriveActiveCapabilities(nil, toolsTyped, []string{"unrecognized_magic_capability"})
	if err == nil || !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for unrecognized capability string, got %v", err)
	}
}

func TestCompiler_UnknownToolAuthority_FailsClosed(t *testing.T) {
	reg := setupTestRegistry(t)
	c := mustNewCompiler(t, reg, nil, nil)
	profile := compiler.MustDefaultProvisionalProfile("ep_1", "chan_1", "model", 32768)

	// 1. Tool schemas provided without declared tools fails closed
	reqNoDeclared := validCompileRequest(profile)
	reqNoDeclared.ToolSchemas = []string{`{"name": "fetch"}`}
	reqNoDeclared.DeclaredTools = nil
	if _, _, err := c.Compile(context.Background(), reqNoDeclared); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument when tool schemas are provided without DeclaredTools, got %v", err)
	}

	// 2. Legacy req.Tools with undeclared tool fails closed
	reqUndeclaredLegacy := validCompileRequest(profile)
	reqUndeclaredLegacy.Tools = []string{"legacy_untyped_tool"}
	reqUndeclaredLegacy.DeclaredTools = []compiler.ToolCapabilityInfo{{Name: "other_tool", ReadOnly: true}}
	if _, _, err := c.Compile(context.Background(), reqUndeclaredLegacy); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for undeclared legacy tool, got %v", err)
	}

	// 3. Tool schema with unrelated declaration fails closed
	reqUnrelated := validCompileRequest(profile)
	reqUnrelated.ToolSchemas = []string{`{"name": "powerful_tool"}`}
	reqUnrelated.DeclaredTools = []compiler.ToolCapabilityInfo{{Name: "unrelated_tool", ReadOnly: true}}
	if _, _, err := c.Compile(context.Background(), reqUnrelated); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for tool schema with unrelated declaration, got %v", err)
	}

	// 4. Declared tool without matching tool schema fails closed
	reqExtraDecl := validCompileRequest(profile)
	reqExtraDecl.ToolSchemas = []string{`{"name": "tool_a"}`}
	reqExtraDecl.DeclaredTools = []compiler.ToolCapabilityInfo{
		{Name: "tool_a", ReadOnly: true},
		{Name: "tool_b", ReadOnly: true},
	}
	if _, _, err := c.Compile(context.Background(), reqExtraDecl); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for extra tool declaration without schema, got %v", err)
	}

	// 5. Mixed modern ToolSchemas and legacy Tools fails closed
	reqMixed := validCompileRequest(profile)
	reqMixed.ToolSchemas = []string{`{"name": "safe_tool"}`}
	reqMixed.Tools = []string{"untyped_legacy_tool"}
	reqMixed.DeclaredTools = []compiler.ToolCapabilityInfo{
		{Name: "safe_tool", ReadOnly: true},
	}
	if _, _, err := c.Compile(context.Background(), reqMixed); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for mixed ToolSchemas and legacy Tools, got %v", err)
	}
}

func TestCompiler_CapabilityAliasNormalization_AdmitsRules(t *testing.T) {
	reg, err := compiler.NewCanonicalRuleRegistry()
	if err != nil {
		t.Fatalf("failed to build canonical rule registry: %v", err)
	}
	c := mustNewCompiler(t, reg, nil, nil)
	profile := compiler.MustDefaultProvisionalProfile("ep_1", "chan_1", "model", 65536)

	// 1. repository_mutation normalizes to write and admits DCI-030, DCI-031, DCI-034
	reqWrite := validCompileRequest(profile)
	reqWrite.SourceRevision = compiler.CanonicalSourceRevision
	reqWrite.MappingVersion = compiler.CanonicalMappingRevision
	reqWrite.ToolSchemas = []string{`{"name": "git_mutator"}`}
	reqWrite.DeclaredTools = []compiler.ToolCapabilityInfo{
		{
			Name:                 "git_mutator",
			RequiredCapabilities: []compiler.CapabilityClass{compiler.CapabilityClassRepositoryMutation},
		},
	}
	_, packWrite, err := c.Compile(context.Background(), reqWrite)
	if err != nil {
		t.Fatalf("compile with repository_mutation failed: %v", err)
	}
	writeRules := map[string]bool{"DCI-030": false, "DCI-031": false, "DCI-034": false}
	for id := range packWrite.AdmittedObjectDigests {
		if _, ok := writeRules[id]; ok {
			writeRules[id] = true
		}
	}
	for id, found := range writeRules {
		if !found {
			t.Errorf("expected %s to be admitted by repository_mutation (normalized to write)", id)
		}
	}

	// 2. process_execution normalizes to exec and admits DCI-033, DCI-083
	reqExec := validCompileRequest(profile)
	reqExec.SourceRevision = compiler.CanonicalSourceRevision
	reqExec.MappingVersion = compiler.CanonicalMappingRevision
	reqExec.ToolSchemas = []string{`{"name": "proc_runner"}`}
	reqExec.DeclaredTools = []compiler.ToolCapabilityInfo{
		{
			Name:                 "proc_runner",
			RequiredCapabilities: []compiler.CapabilityClass{compiler.CapabilityClassProcessExecution},
		},
	}
	_, packExec, err := c.Compile(context.Background(), reqExec)
	if err != nil {
		t.Fatalf("compile with process_execution failed: %v", err)
	}
	execRules := map[string]bool{"DCI-033": false, "DCI-083": false}
	for id := range packExec.AdmittedObjectDigests {
		if _, ok := execRules[id]; ok {
			execRules[id] = true
		}
	}
	for id, found := range execRules {
		if !found {
			t.Errorf("expected %s to be admitted by process_execution (normalized to exec)", id)
		}
	}
}
