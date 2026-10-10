package mcpadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	devcadence "github.com/olostan/DevCadence"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/principal"
	"github.com/olostan/DevCadence/internal/principal/facade"
)

// Config composes one MCP server. The caller is the copied, immutable
// CallerContext of the local launch composition; no request can replace it.
type Config struct {
	Service *facade.Service
	Caller  principal.CallerContext
	Version string
	Logger  *slog.Logger
}

// responder is satisfied by every facade response: each embeds the Envelope.
type responder interface {
	ResponseError() *principal.SemanticError
}

type toolSpec struct {
	name        string
	description string
	call        func(ctx context.Context, caller principal.CallerContext, raw []byte) (any, *principal.SemanticError)
}

// handle builds the shared strict-decode, dispatch, normalise path of one tool.
func handle[Req any, Resp responder](
	svc func(context.Context, principal.CallerContext, Req) (Resp, error),
	decode func([]byte) (Req, error),
) func(context.Context, principal.CallerContext, []byte) (any, *principal.SemanticError) {
	return func(ctx context.Context, caller principal.CallerContext, raw []byte) (any, *principal.SemanticError) {
		req, err := decode(raw)
		if err != nil {
			env := facade.ErrorEnvelope(err)
			return env, env.Error
		}
		resp, err := svc(ctx, caller, req)
		if err != nil {
			env := facade.ErrorEnvelope(err)
			return env, env.Error
		}
		return resp, resp.ResponseError()
	}
}

func tools(s *facade.Service) []toolSpec {
	return []toolSpec{
		{facade.ToolProjectState, "Return the canonical project state (or a focused task/discovery copy) with a live observation of repository drift. Read-only.",
			handle(s.ProjectState, facade.DecodeProjectStateRequest)},
		{facade.ToolInvestigate, "Ask a bounded question about the repository at a base commit. Requires an installed scout runtime; without one the call returns MODEL_UNAVAILABLE. The question is untrusted data.",
			handle(s.Investigate, facade.DecodeInvestigateRequest)},
		{facade.ToolCreateWorkPackage, "Propose an immutable Work Package revision for a designing task. A proposal is not approval, readiness or delegation.",
			handle(s.CreateWorkPackage, facade.DecodeCreateWorkPackageRequest)},
		{facade.ToolDelegate, "Start an approved Work Package. Requires an installed task-execution runtime; without one the call returns MODEL_UNAVAILABLE and changes nothing. A successful connection does not imply delegation works.",
			handle(s.Delegate, facade.DecodeDelegateRequest)},
		{facade.ToolTaskStatus, "Report a task or a process-scoped operation: handles and states only, never logs. A task with a candidate also returns candidate_handoff (commits, durable ref, changed files, model, review label, validation history, inspect commands) when the runtime can inspect it. Operation handles do not survive a restart. Poll a delegate/validate operation handle until status is completed, failed or cancelled.",
			handle(s.TaskStatus, facade.DecodeTaskStatusRequest)},
		{facade.ToolValidate, "Run deterministic validation of a candidate. Requires an installed task-execution runtime; otherwise MODEL_UNAVAILABLE.",
			handle(s.Validate, facade.DecodeValidateRequest)},
		{facade.ToolReview, "Request independent review of a candidate. Requires an installed review runtime; otherwise MODEL_UNAVAILABLE.",
			handle(s.Review, facade.DecodeReviewRequest)},
		{facade.ToolRequestEvidence, "Request bounded evidence: summary, search and symbol return compact structured facts; snippet and diff are worker-mediated and capped at 8192 bytes and 200 lines. No raw file or pager access.",
			handle(s.RequestEvidence, facade.DecodeRequestEvidenceRequest)},
		{facade.ToolAccept, "Acceptance is DISABLED in this release: every well-formed call returns NEEDS_PRINCIPAL and changes nothing. With a self-host runtime the refusal also carries result.handoff, the manual-integration packet for the owner (acceptance is a manual owner action; review was unavailable).",
			handle(s.Accept, facade.DecodeAcceptRequest)},
		{facade.ToolReject, "Send the current reviewing candidate back for repair (ChangeRejected). Starts no new attempt.",
			handle(s.Reject, facade.DecodeRejectRequest)},
		{facade.ToolRecordDecision, "Record an immutable decision. Confirms no human product decision and changes no policy or Git state.",
			handle(s.RecordDecision, facade.DecodeRecordDecisionRequest)},
	}
}

// NewServer builds the MCP server: exactly the eleven base tools, no
// resources, prompts, sampling or roots, and no raw filesystem, shell, SQL or
// network tool.
func NewServer(cfg Config) (*mcp.Server, error) {
	if cfg.Service == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "mcp adapter: facade service is required")
	}
	if err := cfg.Caller.Validate(); err != nil {
		return nil, err
	}
	version := cfg.Version
	if version == "" {
		version = "0.0.0"
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "devcadence-mcp", Version: version}, &mcp.ServerOptions{
		Logger: cfg.Logger,
		// Empty capabilities: no logging, resources, prompts or completions.
		// Tools are advertised because tools are registered below.
		Capabilities: &mcp.ServerCapabilities{},
		Instructions: "DevCadence semantic Principal interface. Acceptance is disabled and executor runtimes may be unavailable; " +
			"read each tool description. The caller identity is bound at launch and cannot be supplied in a request.",
	})
	server.AddReceivingMiddleware(denyUnlistedPrimitives)
	caller := cfg.Caller
	caller.AllowedActions = append([]string(nil), cfg.Caller.AllowedActions...)
	for _, spec := range tools(cfg.Service) {
		schema, err := inputSchema(spec.name)
		if err != nil {
			return nil, err
		}
		call := spec.call
		name := spec.name
		server.AddTool(&mcp.Tool{Name: name, Description: spec.description, InputSchema: schema},
			func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return dispatch(ctx, caller, name, call, req.Params.Arguments), nil
			})
	}
	return server, nil
}

// deniedMethodPrefixes are the protocol primitives this server never offers: a
// raw resource, prompt, completion, logging, sampling, elicitation or roots
// route would be a backdoor around the closed tool allowlist (R6).
var deniedMethodPrefixes = []string{"resources/", "prompts/", "completion/", "logging/", "sampling/", "elicitation/", "roots/"}

// denyUnlistedPrimitives rejects every request for a primitive outside the
// eleven tools, whatever the SDK would otherwise answer.
func denyUnlistedPrimitives(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		for _, prefix := range deniedMethodPrefixes {
			if strings.HasPrefix(method, prefix) {
				return nil, &jsonrpc.Error{Code: jsonrpc.CodeMethodNotFound, Message: "method not supported"}
			}
		}
		return next(ctx, method, req)
	}
}

// dispatch runs one call. A panic or oversize result becomes a complete typed
// error: the transport never emits a truncated or malformed document.
func dispatch(
	ctx context.Context, caller principal.CallerContext, name string,
	call func(context.Context, principal.CallerContext, []byte) (any, *principal.SemanticError),
	raw json.RawMessage,
) (result *mcp.CallToolResult) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = toolResult(facade.ErrorEnvelope(errs.New(errs.CategoryInternal, "tool %s panicked", name)), true)
		}
	}()
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	if len(raw) > facade.MaxRequestBytes {
		return toolResult(facade.ErrorEnvelope(errs.New(errs.CategoryInvalidArgument, "request too large")), true)
	}
	response, failure := call(ctx, caller, raw)
	return toolResult(response, failure != nil)
}

func toolResult(response any, isError bool) *mcp.CallToolResult {
	document, err := json.Marshal(response)
	if err != nil || len(document) > facade.MaxResponseBytes {
		document, _ = json.Marshal(facade.ContextUnfitEnvelope())
		if err != nil {
			document, _ = json.Marshal(facade.ErrorEnvelope(errs.New(errs.CategoryInternal, "marshal")))
		}
		isError = true
	}
	return &mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: string(document)}},
		StructuredContent: json.RawMessage(document),
		IsError:           isError,
	}
}

// inputSchema returns the published request schema of a tool with its
// cross-schema references inlined, so a client needs no schema registry.
func inputSchema(tool string) (any, error) {
	file := "principal-" + strings.ReplaceAll(tool, "_", "-") + "-request.schema.json"
	doc, err := loadSchema(file)
	if err != nil {
		return nil, err
	}
	resolved, err := inline(doc)
	if err != nil {
		return nil, err
	}
	root, ok := resolved.(map[string]any)
	if !ok {
		return nil, errs.New(errs.CategoryInternal, "schema %s is not an object", file)
	}
	delete(root, "$id")
	return root, nil
}

func loadSchema(file string) (any, error) {
	raw, err := devcadence.SchemaFS.ReadFile("schemas/" + file)
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "read schema %s", file)
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "parse schema %s", file)
	}
	return doc, nil
}

const refPrefix = "devcadence:///"

// inline replaces every cross-schema $ref by the referenced schema. A referenced
// schema with local definitions cannot be inlined safely and is refused.
func inline(node any) (any, error) {
	switch typed := node.(type) {
	case map[string]any:
		if ref, ok := typed["$ref"].(string); ok && strings.HasPrefix(ref, refPrefix) && len(typed) == 1 {
			doc, err := loadSchema(strings.TrimPrefix(ref, refPrefix))
			if err != nil {
				return nil, err
			}
			target, ok := doc.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("schema %s is not an object", ref)
			}
			if _, has := target["$defs"]; has {
				return nil, errs.New(errs.CategoryInternal, "schema %s has local definitions and cannot be inlined", ref)
			}
			delete(target, "$schema")
			delete(target, "$id")
			return inline(target)
		}
		out := make(map[string]any, len(typed))
		for k, v := range typed {
			resolved, err := inline(v)
			if err != nil {
				return nil, err
			}
			out[k] = resolved
		}
		return out, nil
	case []any:
		out := make([]any, len(typed))
		for i, v := range typed {
			resolved, err := inline(v)
			if err != nil {
				return nil, err
			}
			out[i] = resolved
		}
		return out, nil
	}
	return node, nil
}
