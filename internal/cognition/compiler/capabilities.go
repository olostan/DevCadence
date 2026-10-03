package compiler

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/protocol"
)

// CapabilityClass represents a recognized authority or execution capability dimension (ADR-0020, AGENTS.md §2).
type CapabilityClass string

const (
	CapabilityClassWrite                CapabilityClass = "write"
	CapabilityClassRepositoryMutation   CapabilityClass = "repository_mutation"
	CapabilityClassExec                 CapabilityClass = "exec"
	CapabilityClassProcessExecution     CapabilityClass = "process_execution"
	CapabilityClassNetwork              CapabilityClass = "network"
	CapabilityClassNetworkAccess        CapabilityClass = "network_access"
	CapabilityClassCredentials          CapabilityClass = "credentials"
	CapabilityClassSpending             CapabilityClass = "spending"
	CapabilityClassDurableStateMutation CapabilityClass = "durable_state_mutation"
	CapabilityClassFilesystem           CapabilityClass = "filesystem"
	CapabilityClassTools                CapabilityClass = "tools"
	CapabilityClassReadOnly             CapabilityClass = "read_only"
)

// Valid reports whether the capability class is recognized.
func (c CapabilityClass) Valid() bool {
	switch c {
	case CapabilityClassWrite, CapabilityClassRepositoryMutation,
		CapabilityClassExec, CapabilityClassProcessExecution,
		CapabilityClassNetwork, CapabilityClassNetworkAccess,
		CapabilityClassCredentials, CapabilityClassSpending,
		CapabilityClassDurableStateMutation, CapabilityClassFilesystem,
		CapabilityClassTools, CapabilityClassReadOnly:
		return true
	}
	return false
}

// Canonical returns the canonical normalized capability name for a CapabilityClass.
// Aliases are mapped to the canonical rule vocabulary:
//   - repository_mutation -> write
//   - process_execution -> exec
//   - network_access -> network
func (c CapabilityClass) Canonical() string {
	switch c {
	case CapabilityClassWrite, CapabilityClassRepositoryMutation:
		return "write"
	case CapabilityClassExec, CapabilityClassProcessExecution:
		return "exec"
	case CapabilityClassNetwork, CapabilityClassNetworkAccess:
		return "network"
	case CapabilityClassCredentials:
		return "credentials"
	case CapabilityClassSpending:
		return "spending"
	case CapabilityClassDurableStateMutation:
		return "durable_state_mutation"
	case CapabilityClassFilesystem:
		return "filesystem"
	case CapabilityClassTools:
		return "tools"
	case CapabilityClassReadOnly:
		return "read_only"
	default:
		return string(c)
	}
}

// NormalizeCapability normalizes a capability string to canonical vocabulary if recognized.
func NormalizeCapability(capName string) (string, error) {
	c := strings.ToLower(strings.TrimSpace(capName))
	capClass := CapabilityClass(c)
	if !capClass.Valid() {
		return "", errs.New(errs.CategoryInvalidArgument, "unrecognized capability %q", capName)
	}
	return capClass.Canonical(), nil
}

// ToolCapabilityInfo defines typed capability metadata for a tool (ADR-0020 §2).
type ToolCapabilityInfo struct {
	Name                 string            `json:"name"`
	RequiredCapabilities []CapabilityClass `json:"required_capabilities,omitempty"`
	ReadOnly             bool              `json:"read_only,omitempty"`
	MutatesFiles         bool              `json:"mutates_files,omitempty"`
}

// Validate checks that the tool capability declaration is valid and typed.
// Fails closed if capability metadata is empty unless explicitly marked ReadOnly.
func (t ToolCapabilityInfo) Validate() error {
	if strings.TrimSpace(t.Name) == "" {
		return errs.New(errs.CategoryInvalidArgument, "tool name cannot be empty")
	}
	if t.ReadOnly && t.MutatesFiles {
		return errs.New(errs.CategoryInvalidArgument,
			"tool %q cannot declare both ReadOnly: true and MutatesFiles: true", t.Name)
	}
	for _, capClass := range t.RequiredCapabilities {
		if !capClass.Valid() {
			return errs.New(errs.CategoryInvalidArgument, "tool %q declares invalid capability %q", t.Name, capClass)
		}
		if t.ReadOnly && capClass.Canonical() != "read_only" {
			return errs.New(errs.CategoryInvalidArgument,
				"tool %q is marked ReadOnly: true but declares privileged capability %q", t.Name, capClass)
		}
	}
	if !t.ReadOnly && len(t.RequiredCapabilities) == 0 && !t.MutatesFiles {
		return errs.New(errs.CategoryInvalidArgument,
			"tool %q has no capabilities specified and is not marked ReadOnly; empty capability metadata is fail-closed", t.Name)
	}
	return nil
}

// DeriveActiveCapabilities deterministically derives active capabilities from session, tool, and channel facts.
// Capabilities are derived from typed metadata and fail closed on unknown inputs.
// Capability aliases are normalized to canonical rule vocabulary.
func DeriveActiveCapabilities(channel *protocol.AccessChannel, tools []ToolCapabilityInfo, declaredCaps []string) ([]string, error) {
	seen := make(map[string]bool)

	if channel != nil {
		if channel.NativeWorktreeAccess {
			seen["write"] = true
			seen["filesystem"] = true
		}
		if channel.CredentialRefID != nil && *channel.CredentialRefID != "" {
			seen["credentials"] = true
		}
		if channel.Kind == protocol.ChannelDirectHTTPAPI || channel.Kind == protocol.ChannelRemoteAgentProxy {
			seen["network"] = true
		}
		if channel.SupportsTools {
			seen["tools"] = true
		}
	}

	for _, tool := range tools {
		if err := tool.Validate(); err != nil {
			return nil, err
		}
		for _, capClass := range tool.RequiredCapabilities {
			seen[capClass.Canonical()] = true
		}
		if tool.MutatesFiles {
			seen["write"] = true
			seen["filesystem"] = true
		}
		if tool.ReadOnly {
			seen["read_only"] = true
		}
	}

	for _, capName := range declaredCaps {
		canon, err := NormalizeCapability(capName)
		if err != nil {
			return nil, err
		}
		seen[canon] = true
	}

	result := make([]string, 0, len(seen))
	for c := range seen {
		result = append(result, c)
	}
	sort.Strings(result)
	return result, nil
}

// extractToolNameFromSchema extracts the tool name from a raw JSON schema definition to support identity-bound tool authority.
func extractToolNameFromSchema(rawSchema string) (string, error) {
	trimmed := strings.TrimSpace(rawSchema)
	if trimmed == "" {
		return "", errs.New(errs.CategoryInvalidArgument, "empty tool schema string")
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &obj); err != nil {
		return "", errs.New(errs.CategoryInvalidArgument, "tool schema is not valid JSON: %v", err)
	}
	if n, ok := obj["name"].(string); ok && strings.TrimSpace(n) != "" {
		return strings.TrimSpace(n), nil
	}
	if fn, ok := obj["function"].(map[string]interface{}); ok {
		if n, ok := fn["name"].(string); ok && strings.TrimSpace(n) != "" {
			return strings.TrimSpace(n), nil
		}
	}
	return "", errs.New(errs.CategoryInvalidArgument, "tool schema lacks a valid 'name' or 'function.name' attribute")
}
