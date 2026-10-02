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

// ToolMediator mediates tool execution, isolating the model from unmediated host access.
type ToolMediator interface {
	// ExecuteTool executes a single tool call within mediated boundaries.
	ExecuteTool(ctx context.Context, call ToolCall) (ToolResult, error)
}

// WorktreeMediator extends ToolMediator with worktree path validation.
type WorktreeMediator interface {
	ToolMediator
	// Scope returns the underlying worktree scope.
	Scope() *tools.Scope
	// ValidatePath checks that relPath resolves strictly within the worktree boundaries.
	ValidatePath(relPath string) (string, error)
}

// ScopedToolMediator implements WorktreeMediator with strict path containment checks
// and registered tool handlers.
type ScopedToolMediator struct {
	scope    *tools.Scope
	handlers map[string]ToolHandler
	mu       sync.RWMutex
}

// NewScopedToolMediator creates a new ScopedToolMediator.
func NewScopedToolMediator(scope *tools.Scope) *ScopedToolMediator {
	return &ScopedToolMediator{
		scope:    scope,
		handlers: make(map[string]ToolHandler),
	}
}

// Scope returns the worktree scope.
func (m *ScopedToolMediator) Scope() *tools.Scope {
	return m.scope
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

// ExecuteTool validates mediation and executes the requested tool.
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
	handler, ok := m.handlers[call.Name]
	m.mu.RUnlock()

	if !ok {
		return ToolResult{
			ToolCallID: call.ID,
			Name:       call.Name,
			Content:    fmt.Sprintf("unknown tool: %q", call.Name),
			IsError:    true,
		}, nil
	}

	content, err := handler(ctx, call.Arguments)
	if err != nil {
		return ToolResult{
			ToolCallID: call.ID,
			Name:       call.Name,
			Content:    err.Error(),
			IsError:    true,
		}, nil
	}

	return ToolResult{
		ToolCallID: call.ID,
		Name:       call.Name,
		Content:    content,
		IsError:    false,
	}, nil
}
