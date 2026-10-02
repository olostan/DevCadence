package drivers

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/tools"
)

// ToolHandler is a function that executes a specific tool given its JSON arguments.
type ToolHandler func(ctx context.Context, args json.RawMessage) (string, error)

// FileEditListener is notified whenever a mediated tool modifies a file in the worktree.
type FileEditListener func(path string, content []byte)

// ToolMediator mediates tool execution, isolating the model from unmediated host access.
type ToolMediator interface {
	// ExecuteTool executes a single tool call within mediated boundaries.
	ExecuteTool(ctx context.Context, call ToolCall) (ToolResult, error)
	// SetDeclaredTools restricts tool execution to declared tool names.
	SetDeclaredTools(tools []ToolDefinition)
	// OnFileEdit registers a listener for file mutations (used by metering for loop detection).
	OnFileEdit(listener FileEditListener)
}

// WorktreeMediator extends ToolMediator with worktree path validation.
type WorktreeMediator interface {
	ToolMediator
	// Scope returns the underlying worktree scope.
	Scope() *tools.Scope
	// ValidatePath checks that relPath resolves strictly within the worktree boundaries.
	ValidatePath(relPath string) (string, error)
}

// ScopedToolMediator implements WorktreeMediator with strict path containment checks,
// declared-tool filtering, and registered tool handlers.
type ScopedToolMediator struct {
	scope         *tools.Scope
	handlers      map[string]ToolHandler
	declaredTools map[string]struct{}
	editListeners []FileEditListener
	mu            sync.RWMutex
}

// NewScopedToolMediator creates a new ScopedToolMediator.
func NewScopedToolMediator(scope *tools.Scope) *ScopedToolMediator {
	return &ScopedToolMediator{
		scope:         scope,
		handlers:      make(map[string]ToolHandler),
		editListeners: make([]FileEditListener, 0),
	}
}

// Scope returns the worktree scope.
func (m *ScopedToolMediator) Scope() *tools.Scope {
	return m.scope
}

// SetDeclaredTools restricts tool execution to only the declared tools.
func (m *ScopedToolMediator) SetDeclaredTools(tools []ToolDefinition) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.declaredTools = make(map[string]struct{}, len(tools))
	for _, t := range tools {
		m.declaredTools[t.Name] = struct{}{}
	}
}

// OnFileEdit registers a listener invoked when a file is modified through the mediator.
func (m *ScopedToolMediator) OnFileEdit(listener FileEditListener) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.editListeners = append(m.editListeners, listener)
}

// RegisterHandler registers a handler for a named tool.
func (m *ScopedToolMediator) RegisterHandler(name string, handler ToolHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers[name] = handler
}

// ValidatePath ensures that relPath does not escape the worktree scope.
func (m *ScopedToolMediator) ValidatePath(relPath string) (string, error) {
	if m.scope == nil || m.scope.WorktreePath == "" {
		return "", errs.New(errs.CategoryInvalidArgument, "mediator: worktree scope is not configured")
	}
	return m.scope.ResolvePath(relPath)
}

// extractPathsFromArgs inspects JSON arguments for common path-bearing parameter names.
func extractPathsFromArgs(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}

	pathKeys := []string{
		"path", "file", "filepath", "file_path", "rel_path",
		"target_file", "source_file", "target", "directory", "dir",
	}

	var foundPaths []string
	for _, k := range pathKeys {
		if val, exists := obj[k]; exists {
			if s, ok := val.(string); ok && s != "" {
				foundPaths = append(foundPaths, s)
			}
		}
	}
	return foundPaths
}

// extractContentFromArgs checks for content payload in edit/write operations.
func extractContentFromArgs(raw json.RawMessage) ([]byte, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, false
	}

	contentKeys := []string{"content", "code_content", "text", "replacement_content"}
	for _, k := range contentKeys {
		if val, exists := obj[k]; exists {
			if s, ok := val.(string); ok {
				return []byte(s), true
			}
		}
	}
	return nil, false
}

// ExecuteTool validates declared tools, structurally enforces worktree containment on
// path-bearing parameters, executes the tool, and notifies edit listeners on mutations.
func (m *ScopedToolMediator) ExecuteTool(ctx context.Context, call ToolCall) (ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return ToolResult{
			ToolCallID: call.ID,
			Name:       call.Name,
			Content:    err.Error(),
			IsError:    true,
		}, err
	}

	m.mu.RLock()
	// 1. Authorize: Only declared tools in SessionConfig.Tools may be executed
	if m.declaredTools != nil {
		if _, authorized := m.declaredTools[call.Name]; !authorized {
			m.mu.RUnlock()
			err := errs.New(errs.CategoryPolicyDenied, "mediator: tool %q is not declared or authorized in session configuration", call.Name)
			return ToolResult{
				ToolCallID: call.ID,
				Name:       call.Name,
				Content:    err.Error(),
				IsError:    true,
			}, err
		}
	}

	handler, ok := m.handlers[call.Name]
	scope := m.scope
	m.mu.RUnlock()

	if !ok {
		return ToolResult{
			ToolCallID: call.ID,
			Name:       call.Name,
			Content:    fmt.Sprintf("unknown tool: %q", call.Name),
			IsError:    true,
		}, nil
	}

	// 2. Structurally enforce worktree path containment before handler execution
	if scope != nil && scope.WorktreePath != "" {
		paths := extractPathsFromArgs(call.Arguments)
		for _, p := range paths {
			if _, err := scope.ResolvePath(p); err != nil {
				deniedErr := errs.Wrap(errs.CategoryPolicyDenied, err, "mediator: path %q violates worktree containment", p)
				return ToolResult{
					ToolCallID: call.ID,
					Name:       call.Name,
					Content:    deniedErr.Error(),
					IsError:    true,
				}, deniedErr
			}
		}
	}

	// 3. Execute tool handler
	content, err := handler(ctx, call.Arguments)
	if err != nil {
		return ToolResult{
			ToolCallID: call.ID,
			Name:       call.Name,
			Content:    err.Error(),
			IsError:    true,
		}, nil
	}

	// 4. If mutation occurred, notify edit listeners for semantic loop detection
	paths := extractPathsFromArgs(call.Arguments)
	if len(paths) > 0 {
		editBytes, hasContent := extractContentFromArgs(call.Arguments)
		if !hasContent {
			editBytes = []byte(content)
		}
		m.mu.RLock()
		listeners := append([]FileEditListener(nil), m.editListeners...)
		m.mu.RUnlock()
		for _, l := range listeners {
			l(paths[0], editBytes)
		}
	}

	return ToolResult{
		ToolCallID: call.ID,
		Name:       call.Name,
		Content:    content,
		IsError:    false,
	}, nil
}
