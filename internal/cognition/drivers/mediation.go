package drivers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/tools"
)

// ToolHandler is a function that executes a specific tool given its JSON arguments.
type ToolHandler func(ctx context.Context, args json.RawMessage) (string, error)

// FileEditListener is notified whenever a mediated tool modifies a file in the worktree.
type FileEditListener func(path string, content []byte)

// ToolExecutionListener is notified whenever any tool call is executed through the mediator.
type ToolExecutionListener func(call ToolCall, res ToolResult)

// ToolMediator mediates tool execution, isolating the model from unmediated host access.
type ToolMediator interface {
	// ExecuteTool executes a single tool call within mediated boundaries.
	ExecuteTool(ctx context.Context, call ToolCall) (ToolResult, error)
	// SetDeclaredTools restricts tool execution to declared tool names.
	SetDeclaredTools(tools []ToolDefinition)
	// OnFileEdit registers a listener for file mutations (used by metering for loop detection).
	OnFileEdit(listener FileEditListener)
	// OnToolExecution registers a listener for all executed tools (used by metering for loop detection).
	OnToolExecution(listener ToolExecutionListener)
	// AttachMeterListeners registers or updates file edit and tool execution listeners for a specific meter ID idempotently.
	AttachMeterListeners(meterID string, onEdit FileEditListener, onExec ToolExecutionListener)
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
	scope              *tools.Scope
	handlers           map[string]ToolHandler
	toolDefs           map[string]ToolDefinition
	declaredTools      map[string]ToolDefinition
	editListeners      []FileEditListener
	execListeners      []ToolExecutionListener
	meterEditListeners map[string]FileEditListener
	meterExecListeners map[string]ToolExecutionListener
	sessions           map[string]*SessionScopedMediator
	mu                 sync.RWMutex
}

// NewScopedToolMediator creates a new ScopedToolMediator.
func NewScopedToolMediator(scope *tools.Scope) *ScopedToolMediator {
	return &ScopedToolMediator{
		scope:              scope,
		handlers:           make(map[string]ToolHandler),
		toolDefs:           make(map[string]ToolDefinition),
		editListeners:      make([]FileEditListener, 0),
		execListeners:      make([]ToolExecutionListener, 0),
		meterEditListeners: make(map[string]FileEditListener),
		meterExecListeners: make(map[string]ToolExecutionListener),
		sessions:           make(map[string]*SessionScopedMediator),
	}
}

// Scope returns the worktree scope.
func (m *ScopedToolMediator) Scope() *tools.Scope {
	return m.scope
}

// RegisterToolDefinition registers a tool definition with its schema and path configuration.
func (m *ScopedToolMediator) RegisterToolDefinition(def ToolDefinition) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toolDefs[def.Name] = def
}

// RegisterHandler registers a handler for a named tool.
func (m *ScopedToolMediator) RegisterHandler(name string, handler ToolHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers[name] = handler
}

// ForSession creates a session-isolated ToolMediator instance with its own declared tools,
// listeners, and session scope, preventing shared mutable state across sessions.
func (m *ScopedToolMediator) ForSession(sessionID string, declaredTools []ToolDefinition) *SessionScopedMediator {
	m.mu.Lock()

	toolsMap := make(map[string]ToolDefinition, len(declaredTools))
	for _, t := range declaredTools {
		// Inherit path definitions and mutation flag from registered definitions if not explicitly provided
		if def, ok := m.toolDefs[t.Name]; ok {
			if len(t.PathParameters) == 0 {
				t.PathParameters = def.PathParameters
			}
			if t.PathExtractor == nil {
				t.PathExtractor = def.PathExtractor
			}
			if !t.MutatesFiles && def.MutatesFiles {
				t.MutatesFiles = def.MutatesFiles
			}
		}
		toolsMap[t.Name] = t
	}

	existing, ok := m.sessions[sessionID]
	if ok {
		m.mu.Unlock()
		existing.mu.Lock()
		existing.declaredTools = toolsMap
		existing.mu.Unlock()
		return existing
	}

	sessionMediator := &SessionScopedMediator{
		parent:             m,
		sessionID:          sessionID,
		declaredTools:      toolsMap,
		editListeners:      make([]FileEditListener, 0),
		execListeners:      make([]ToolExecutionListener, 0),
		meterEditListeners: make(map[string]FileEditListener),
		meterExecListeners: make(map[string]ToolExecutionListener),
	}
	m.sessions[sessionID] = sessionMediator
	m.mu.Unlock()
	return sessionMediator
}

// SetDeclaredTools restricts tool execution to only the declared tools.
func (m *ScopedToolMediator) SetDeclaredTools(tools []ToolDefinition) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.declaredTools = make(map[string]ToolDefinition, len(tools))
	for _, t := range tools {
		if def, ok := m.toolDefs[t.Name]; ok {
			if len(t.PathParameters) == 0 {
				t.PathParameters = def.PathParameters
			}
			if t.PathExtractor == nil {
				t.PathExtractor = def.PathExtractor
			}
			if !t.MutatesFiles && def.MutatesFiles {
				t.MutatesFiles = def.MutatesFiles
			}
		}
		m.declaredTools[t.Name] = t
	}
}

// OnFileEdit registers a listener invoked when a file is modified through the mediator.
func (m *ScopedToolMediator) OnFileEdit(listener FileEditListener) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.editListeners = append(m.editListeners, listener)
}

// OnToolExecution registers a listener invoked on every tool execution attempt and result.
func (m *ScopedToolMediator) OnToolExecution(listener ToolExecutionListener) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.execListeners = append(m.execListeners, listener)
}

// AttachMeterListeners registers or updates file edit and tool execution listeners for a specific meter ID idempotently.
func (m *ScopedToolMediator) AttachMeterListeners(meterID string, onEdit FileEditListener, onExec ToolExecutionListener) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if onEdit != nil {
		m.meterEditListeners[meterID] = onEdit
	}
	if onExec != nil {
		m.meterExecListeners[meterID] = onExec
	}
}

// ValidatePath ensures that relPath does not escape the worktree scope.
func (m *ScopedToolMediator) ValidatePath(relPath string) (string, error) {
	if m.scope == nil || m.scope.WorktreePath == "" {
		return "", errs.New(errs.CategoryInvalidArgument, "mediator: worktree scope is not configured")
	}
	return m.scope.ResolvePath(relPath)
}

// ExecuteTool executes a tool call using the mediator's registered handlers.
func (m *ScopedToolMediator) ExecuteTool(ctx context.Context, call ToolCall) (ToolResult, error) {
	m.mu.RLock()
	var toolDef *ToolDefinition
	if m.declaredTools != nil {
		if def, authorized := m.declaredTools[call.Name]; authorized {
			toolDef = &def
		} else {
			m.mu.RUnlock()
			err := errs.New(errs.CategoryPolicyDenied, "mediator: tool %q is not declared or authorized in session configuration", call.Name)
			res := ToolResult{
				ToolCallID: call.ID,
				Name:       call.Name,
				Content:    err.Error(),
				IsError:    true,
			}
			m.notifyExecution(call, res)
			return res, err
		}
	} else if def, exists := m.toolDefs[call.Name]; exists {
		toolDef = &def
	}

	handler, ok := m.handlers[call.Name]
	scope := m.scope
	m.mu.RUnlock()

	return m.executeInternal(ctx, call, toolDef, handler, ok, scope, m.notifyEdit, m.notifyExecution)
}

func (m *ScopedToolMediator) notifyEdit(path string, content []byte) {
	m.mu.RLock()
	listeners := append([]FileEditListener(nil), m.editListeners...)
	for _, l := range m.meterEditListeners {
		listeners = append(listeners, l)
	}
	var childSessions []*SessionScopedMediator
	for _, s := range m.sessions {
		childSessions = append(childSessions, s)
	}
	m.mu.RUnlock()

	for _, l := range listeners {
		l(path, content)
	}
	for _, s := range childSessions {
		s.notifyEditLocal(path, content)
	}
}

func (m *ScopedToolMediator) notifyExecution(call ToolCall, res ToolResult) {
	m.mu.RLock()
	listeners := append([]ToolExecutionListener(nil), m.execListeners...)
	for _, l := range m.meterExecListeners {
		listeners = append(listeners, l)
	}
	var childSessions []*SessionScopedMediator
	for _, s := range m.sessions {
		childSessions = append(childSessions, s)
	}
	m.mu.RUnlock()

	for _, l := range listeners {
		l(call, res)
	}
	for _, s := range childSessions {
		s.notifyExecutionLocal(call, res)
	}
}

func (m *ScopedToolMediator) notifyEditParentOnly(path string, content []byte) {
	m.mu.RLock()
	listeners := append([]FileEditListener(nil), m.editListeners...)
	for _, l := range m.meterEditListeners {
		listeners = append(listeners, l)
	}
	m.mu.RUnlock()
	for _, l := range listeners {
		l(path, content)
	}
}

func (m *ScopedToolMediator) notifyExecutionParentOnly(call ToolCall, res ToolResult) {
	m.mu.RLock()
	listeners := append([]ToolExecutionListener(nil), m.execListeners...)
	for _, l := range m.meterExecListeners {
		listeners = append(listeners, l)
	}
	m.mu.RUnlock()
	for _, l := range listeners {
		l(call, res)
	}
}

// SessionScopedMediator provides session-isolated tool execution, declared tools, and listeners.
type SessionScopedMediator struct {
	parent             *ScopedToolMediator
	sessionID          string
	declaredTools      map[string]ToolDefinition
	editListeners      []FileEditListener
	execListeners      []ToolExecutionListener
	meterEditListeners map[string]FileEditListener
	meterExecListeners map[string]ToolExecutionListener
	mu                 sync.RWMutex
}

// Scope returns the underlying worktree scope.
func (s *SessionScopedMediator) Scope() *tools.Scope {
	return s.parent.Scope()
}

// ValidatePath ensures that relPath does not escape the worktree scope.
func (s *SessionScopedMediator) ValidatePath(relPath string) (string, error) {
	return s.parent.ValidatePath(relPath)
}

// SetDeclaredTools restricts tool execution to declared tools.
func (s *SessionScopedMediator) SetDeclaredTools(tools []ToolDefinition) {
	s.parent.mu.RLock()
	parentDefs := make(map[string]ToolDefinition, len(tools))
	for _, t := range tools {
		if def, ok := s.parent.toolDefs[t.Name]; ok {
			parentDefs[t.Name] = def
		}
	}
	s.parent.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.declaredTools = make(map[string]ToolDefinition, len(tools))
	for _, t := range tools {
		if def, ok := parentDefs[t.Name]; ok {
			if len(t.PathParameters) == 0 {
				t.PathParameters = def.PathParameters
			}
			if t.PathExtractor == nil {
				t.PathExtractor = def.PathExtractor
			}
			if !t.MutatesFiles && def.MutatesFiles {
				t.MutatesFiles = def.MutatesFiles
			}
		}
		s.declaredTools[t.Name] = t
	}
}

// OnFileEdit registers a session-scoped file mutation listener.
func (s *SessionScopedMediator) OnFileEdit(listener FileEditListener) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.editListeners = append(s.editListeners, listener)
}

// OnToolExecution registers a session-scoped tool execution listener.
func (s *SessionScopedMediator) OnToolExecution(listener ToolExecutionListener) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.execListeners = append(s.execListeners, listener)
}

// AttachMeterListeners registers or updates session-scoped file edit and tool execution listeners for a meter ID idempotently.
func (s *SessionScopedMediator) AttachMeterListeners(meterID string, onEdit FileEditListener, onExec ToolExecutionListener) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if onEdit != nil {
		s.meterEditListeners[meterID] = onEdit
	}
	if onExec != nil {
		s.meterExecListeners[meterID] = onExec
	}
}

// ExecuteTool validates declared tools, path containment, and executes the tool.
func (s *SessionScopedMediator) ExecuteTool(ctx context.Context, call ToolCall) (ToolResult, error) {
	s.mu.RLock()
	var toolDef *ToolDefinition
	if s.declaredTools != nil {
		if def, authorized := s.declaredTools[call.Name]; authorized {
			toolDefCopy := def
			toolDef = &toolDefCopy
		} else {
			s.mu.RUnlock()
			err := errs.New(errs.CategoryPolicyDenied, "mediator: tool %q is not declared or authorized in session configuration", call.Name)
			res := ToolResult{
				ToolCallID: call.ID,
				Name:       call.Name,
				Content:    err.Error(),
				IsError:    true,
			}
			s.notifyExecution(call, res)
			return res, err
		}
	}
	s.mu.RUnlock()

	s.parent.mu.RLock()
	if toolDef == nil {
		if def, exists := s.parent.toolDefs[call.Name]; exists {
			toolDefCopy := def
			toolDef = &toolDefCopy
		}
	}
	handler, ok := s.parent.handlers[call.Name]
	scope := s.parent.scope
	s.parent.mu.RUnlock()

	return s.parent.executeInternal(ctx, call, toolDef, handler, ok, scope, s.notifyEdit, s.notifyExecution)
}

func (s *SessionScopedMediator) notifyEdit(path string, content []byte) {
	s.notifyEditLocal(path, content)
	s.parent.notifyEditParentOnly(path, content)
}

func (s *SessionScopedMediator) notifyEditLocal(path string, content []byte) {
	s.mu.RLock()
	listeners := append([]FileEditListener(nil), s.editListeners...)
	for _, l := range s.meterEditListeners {
		listeners = append(listeners, l)
	}
	s.mu.RUnlock()
	for _, l := range listeners {
		l(path, content)
	}
}

func (s *SessionScopedMediator) notifyExecution(call ToolCall, res ToolResult) {
	s.notifyExecutionLocal(call, res)
	s.parent.notifyExecutionParentOnly(call, res)
}

func (s *SessionScopedMediator) notifyExecutionLocal(call ToolCall, res ToolResult) {
	s.mu.RLock()
	listeners := append([]ToolExecutionListener(nil), s.execListeners...)
	for _, l := range s.meterExecListeners {
		listeners = append(listeners, l)
	}
	s.mu.RUnlock()
	for _, l := range listeners {
		l(call, res)
	}
}

func (m *ScopedToolMediator) executeInternal(
	ctx context.Context,
	call ToolCall,
	toolDef *ToolDefinition,
	handler ToolHandler,
	handlerExists bool,
	scope *tools.Scope,
	notifyEdit func(path string, content []byte),
	notifyExecution func(call ToolCall, res ToolResult),
) (ToolResult, error) {
	if err := ctx.Err(); err != nil {
		res := ToolResult{
			ToolCallID: call.ID,
			Name:       call.Name,
			Content:    err.Error(),
			IsError:    true,
		}
		notifyExecution(call, res)
		return res, err
	}

	if !handlerExists {
		res := ToolResult{
			ToolCallID: call.ID,
			Name:       call.Name,
			Content:    fmt.Sprintf("unknown tool: %q", call.Name),
			IsError:    true,
		}
		notifyExecution(call, res)
		return res, nil
	}

	// 1. Structurally enforce worktree path containment before handler execution
	if scope != nil && scope.WorktreePath != "" {
		paths, extractErr := extractPathsFromArgs(call.Arguments, toolDef)
		if extractErr != nil {
			deniedErr := errs.Wrap(errs.CategoryPolicyDenied, extractErr, "mediator: failed extracting paths for tool %q", call.Name)
			res := ToolResult{
				ToolCallID: call.ID,
				Name:       call.Name,
				Content:    deniedErr.Error(),
				IsError:    true,
			}
			notifyExecution(call, res)
			return res, deniedErr
		}
		for _, p := range paths {
			if _, err := scope.ResolvePath(p); err != nil {
				deniedErr := errs.Wrap(errs.CategoryPolicyDenied, err, "mediator: path %q violates worktree containment", p)
				res := ToolResult{
					ToolCallID: call.ID,
					Name:       call.Name,
					Content:    deniedErr.Error(),
					IsError:    true,
				}
				notifyExecution(call, res)
				return res, deniedErr
			}
		}
	}

	// 2. Execute tool handler
	content, err := handler(ctx, call.Arguments)
	if err != nil {
		res := ToolResult{
			ToolCallID: call.ID,
			Name:       call.Name,
			Content:    err.Error(),
			IsError:    true,
		}
		notifyExecution(call, res)
		return res, nil
	}

	res := ToolResult{
		ToolCallID: call.ID,
		Name:       call.Name,
		Content:    content,
		IsError:    false,
	}

	// 3. If mutation occurred, notify edit listeners for semantic loop detection
	// Only tools declaring MutatesFiles: true trigger file edit listeners (ADR-0019 §2)
	if toolDef != nil && toolDef.MutatesFiles {
		paths, _ := extractPathsFromArgs(call.Arguments, toolDef)
		if len(paths) > 0 {
			editBytes, hasContent := extractContentFromArgs(call.Arguments)
			if !hasContent {
				editBytes = []byte(content)
			}
			for _, p := range paths {
				notifyEdit(p, editBytes)
			}
		}
	}

	notifyExecution(call, res)
	return res, nil
}

// extractPathsFromArgs inspects JSON arguments for path-bearing parameters using
// custom path extractors, declared path parameters, and recursive JSON inspection.
func extractPathsFromArgs(raw json.RawMessage, toolDef *ToolDefinition) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	// 1. Tool-provided custom PathExtractor takes precedence
	if toolDef != nil && toolDef.PathExtractor != nil {
		return toolDef.PathExtractor(raw)
	}

	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, nil
	}

	var foundPaths []string

	// 2. If ToolDefinition declares explicit PathParameters, extract those
	if toolDef != nil && len(toolDef.PathParameters) > 0 {
		if obj, ok := root.(map[string]any); ok {
			for _, param := range toolDef.PathParameters {
				extractPathsByParam(obj, param, &foundPaths)
			}
		}
	}

	// 3. Robust recursive/structural inspection traversing objects and arrays
	recursiveExtractPaths(root, &foundPaths)

	// Deduplicate preserving order
	seen := make(map[string]struct{}, len(foundPaths))
	var uniquePaths []string
	for _, p := range foundPaths {
		if p == "" {
			continue
		}
		if _, exists := seen[p]; !exists {
			seen[p] = struct{}{}
			uniquePaths = append(uniquePaths, p)
		}
	}

	return uniquePaths, nil
}

func isPathKey(key string) bool {
	k := strings.ToLower(key)
	switch k {
	case "path", "file", "filepath", "file_path", "rel_path",
		"target_file", "source_file", "target", "destination", "dest",
		"directory", "dir", "root", "folder", "base_dir",
		"paths", "files", "filepaths", "file_paths":
		return true
	}
	if strings.HasSuffix(k, "_path") || strings.HasSuffix(k, "_file") || strings.HasSuffix(k, "_dir") ||
		strings.HasSuffix(k, "_paths") || strings.HasSuffix(k, "_files") {
		return true
	}
	return false
}

func recursiveExtractPaths(node any, paths *[]string) {
	switch v := node.(type) {
	case map[string]any:
		for k, val := range v {
			if isPathKey(k) {
				switch s := val.(type) {
				case string:
					if s != "" {
						*paths = append(*paths, s)
					}
				case []any:
					for _, item := range s {
						if str, ok := item.(string); ok && str != "" {
							*paths = append(*paths, str)
						}
					}
				}
			}
			recursiveExtractPaths(val, paths)
		}
	case []any:
		for _, item := range v {
			recursiveExtractPaths(item, paths)
		}
	}
}

func extractPathsByParam(obj map[string]any, param string, paths *[]string) {
	parts := strings.Split(param, ".")
	var current any = obj
	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return
		}
		current = m[part]
	}
	switch v := current.(type) {
	case string:
		if v != "" {
			*paths = append(*paths, v)
		}
	case []any:
		for _, item := range v {
			if str, ok := item.(string); ok && str != "" {
				*paths = append(*paths, str)
			}
		}
	}
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
