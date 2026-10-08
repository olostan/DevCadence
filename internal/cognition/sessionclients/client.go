package sessionclients

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/olostan/DevCadence/internal/cognition/drivers"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/execpolicy"
)

var _ drivers.DirectAPIClient = (*LoopbackClient)(nil)

// LoopbackClient implements drivers.DirectAPIClient for loopback Ollama instances.
type LoopbackClient struct {
	baseURL    string
	modelID    string
	limits     execpolicy.ExecutionLimits
	httpClient *http.Client
}

// NewLoopbackClient constructs a LoopbackClient bound to a validated loopback address.
func NewLoopbackClient(baseURL string, modelID string, limits execpolicy.ExecutionLimits, httpClient *http.Client) (*LoopbackClient, error) {
	if baseURL == "" {
		baseURL = DefaultLoopbackBaseURL
	}
	if err := validateLoopbackURL(baseURL); err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient = newLoopbackHTTPClient()
	} else {
		// Enforce no-proxy policy if an explicit transport is configured.
		if t, ok := httpClient.Transport.(*http.Transport); ok && t != nil {
			if t.Proxy != nil {
				return nil, errs.New(errs.CategoryPolicyDenied, "proxy configuration is forbidden for loopback client")
			}
		}
	}

	return &LoopbackClient{
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		modelID:    modelID,
		limits:     limits,
		httpClient: httpClient,
	}, nil
}

// validateLoopbackURL ensures the URL is an HTTP loopback address with no credentials.
func validateLoopbackURL(rawURL string) error {
	if strings.TrimSpace(rawURL) == "" {
		return errs.New(errs.CategoryInvalidArgument, "loopback URL cannot be empty")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return errs.Wrap(errs.CategoryInvalidArgument, err, "invalid loopback URL %q", rawURL)
	}
	if u.Scheme != "http" {
		return errs.New(errs.CategoryPolicyDenied, "loopback URL scheme must be http, got %q", u.Scheme)
	}
	if u.User != nil {
		return errs.New(errs.CategoryPolicyDenied, "credentials are forbidden in loopback URL %q", rawURL)
	}
	host := u.Hostname()
	if host == "" {
		return errs.New(errs.CategoryInvalidArgument, "loopback URL %q has empty host", rawURL)
	}
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1":
		return nil
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip != nil && ip.IsLoopback() {
		return nil
	}
	return errs.New(errs.CategoryPolicyDenied, "%q is not a loopback address", rawURL)
}

// newLoopbackHTTPClient returns an http.Client with strictly loopback dialing and fail-closed redirects.
func newLoopbackHTTPClient() *http.Client {
	transport := &http.Transport{
		Proxy: nil, // strictly no proxy
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, errs.New(errs.CategoryInvalidArgument, "invalid network address: %s", addr)
			}
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, errs.Wrap(errs.CategoryModelUnavailable, err, "failed to resolve host %s", host)
			}
			if len(ips) == 0 {
				return nil, errs.New(errs.CategoryModelUnavailable, "no IP address found for %s", host)
			}
			for _, ip := range ips {
				if !ip.IsLoopback() {
					return nil, errs.New(errs.CategoryPolicyDenied, "address %s resolves to non-loopback IP %s", host, ip.String())
				}
			}
			var dialer net.Dialer
			conn, err := dialer.DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			remoteHost, _, err := net.SplitHostPort(conn.RemoteAddr().String())
			if err == nil {
				remoteIP := net.ParseIP(remoteHost)
				if remoteIP == nil || !remoteIP.IsLoopback() {
					_ = conn.Close()
					return nil, errs.New(errs.CategoryPolicyDenied, "dialed remote address %s is not loopback", conn.RemoteAddr().String())
				}
			}
			return conn, nil
		},
	}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return errs.New(errs.CategoryPolicyDenied, "redirects are forbidden for loopback client")
		},
	}
}

type ollamaMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
}

type ollamaToolCall struct {
	Function ollamaFunctionCall `json:"function"`
}

type ollamaFunctionCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type ollamaTool struct {
	Type     string            `json:"type"`
	Function ollamaFunctionDef `json:"function"`
}

type ollamaFunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Tools    []ollamaTool    `json:"tools,omitempty"`
	Stream   bool            `json:"stream"`
	Options  *ollamaOptions  `json:"options,omitempty"`
}

type ollamaOptions struct {
	NumPredict int64 `json:"num_predict"`
}

type ollamaChatResponse struct {
	Model     string `json:"model"`
	CreatedAt string `json:"created_at"`
	Message   struct {
		Role      string `json:"role"`
		Content   string `json:"content"`
		ToolCalls []struct {
			Function struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
	} `json:"message"`
	Done            bool   `json:"done"`
	TotalDuration   int64  `json:"total_duration"`
	PromptEvalCount *int64 `json:"prompt_eval_count"`
	EvalCount       *int64 `json:"eval_count"`
}

// Complete executes a non-streaming completion turn using Ollama's POST /api/chat.
func (c *LoopbackClient) Complete(ctx context.Context, req drivers.DirectAPIRequest) (drivers.DirectAPIResponse, error) {
	if c.limits.MaxDurationSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(c.limits.MaxDurationSeconds)*time.Second)
		defer cancel()
	}

	model := req.ModelID
	if model == "" {
		model = c.modelID
	}

	var messages []ollamaMessage
	if req.SystemPrompt != "" {
		hasSystem := len(req.Messages) > 0 && req.Messages[0].Role == "system"
		if !hasSystem {
			messages = append(messages, ollamaMessage{
				Role:    "system",
				Content: req.SystemPrompt,
			})
		}
	}

	for _, m := range req.Messages {
		role := m.Role
		if role == "" {
			role = "user"
		}
		content := m.Content
		if m.ToolResult != nil {
			role = "tool"
			if content == "" {
				content = m.ToolResult.Content
			}
		}

		var toolCalls []ollamaToolCall
		for _, tc := range m.ToolCalls {
			toolCalls = append(toolCalls, ollamaToolCall{
				Function: ollamaFunctionCall{
					Name:      tc.Name,
					Arguments: tc.Arguments,
				},
			})
		}

		messages = append(messages, ollamaMessage{
			Role:      role,
			Content:   content,
			ToolCalls: toolCalls,
		})
	}

	if len(messages) == 0 && req.Prompt != "" {
		messages = append(messages, ollamaMessage{
			Role:    "user",
			Content: req.Prompt,
		})
	}
	if messages == nil {
		messages = []ollamaMessage{}
	}

	var tools []ollamaTool
	for _, t := range req.Tools {
		params := t.Parameters
		if len(params) == 0 {
			params = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		tools = append(tools, ollamaTool{
			Type: "function",
			Function: ollamaFunctionDef{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  params,
			},
		})
	}

	numPredict := c.limits.MaxOutputTokensPerCall
	if numPredict == 0 && req.MaxOutputTokens > 0 {
		numPredict = req.MaxOutputTokens
	}

	chatReq := ollamaChatRequest{
		Model:    model,
		Messages: messages,
		Tools:    tools,
		Stream:   false,
		Options: &ollamaOptions{
			NumPredict: numPredict,
		},
	}

	payloadBytes, err := json.Marshal(chatReq)
	if err != nil {
		return drivers.DirectAPIResponse{}, errs.Wrap(errs.CategoryInvalidArgument, err, "marshal chat request")
	}

	if c.limits.MaxRequestBytes > 0 && len(payloadBytes) > c.limits.MaxRequestBytes {
		return drivers.DirectAPIResponse{}, errs.New(errs.CategoryInvalidArgument,
			"request body size %d exceeds max_request_bytes limit %d", len(payloadBytes), c.limits.MaxRequestBytes)
	}
	if len(payloadBytes) > MaxRequestBytes {
		return drivers.DirectAPIResponse{}, errs.New(errs.CategoryInvalidArgument,
			"request body size %d exceeds 1 MiB limit", len(payloadBytes))
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(payloadBytes))
	if err != nil {
		return drivers.DirectAPIResponse{}, errs.Wrap(errs.CategoryInvalidArgument, err, "build chat request")
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return drivers.DirectAPIResponse{}, errs.Wrap(errs.CategoryProbeTimeout, err, "ollama chat request timed out or cancelled")
		}
		return drivers.DirectAPIResponse{}, errs.Wrap(errs.CategoryModelUnavailable, err, "ollama chat request failed")
	}
	defer func() { _ = httpResp.Body.Close() }()

	respBytes, err := io.ReadAll(io.LimitReader(httpResp.Body, MaxResponseBytes+1))
	if err != nil {
		return drivers.DirectAPIResponse{}, errs.Wrap(errs.CategoryProbeFailed, err, "read chat response")
	}
	if len(respBytes) > MaxResponseBytes {
		return drivers.DirectAPIResponse{}, errs.New(errs.CategoryProbeFailed, "response body size %d exceeds 4 MiB limit", len(respBytes))
	}

	if httpResp.StatusCode != http.StatusOK {
		return drivers.DirectAPIResponse{}, errs.New(errs.CategoryModelUnavailable,
			"ollama chat returned status %d: %s", httpResp.StatusCode, string(respBytes))
	}

	var chatResp ollamaChatResponse
	if err := json.Unmarshal(respBytes, &chatResp); err != nil {
		return drivers.DirectAPIResponse{}, errs.Wrap(errs.CategoryProbeFailed, err, "decode chat response")
	}

	var mappedToolCalls []drivers.ToolCall
	if len(chatResp.Message.ToolCalls) > 0 {
		requestedTools := make(map[string]bool, len(req.Tools))
		for _, t := range req.Tools {
			requestedTools[t.Name] = true
		}
		for i, tc := range chatResp.Message.ToolCalls {
			name := tc.Function.Name
			if !requestedTools[name] {
				return drivers.DirectAPIResponse{}, errs.New(errs.CategoryIntegrity,
					"tool call %q was not in requested tool list", name)
			}
			var argsMap map[string]any
			if err := json.Unmarshal(tc.Function.Arguments, &argsMap); err != nil || argsMap == nil {
				return drivers.DirectAPIResponse{}, errs.New(errs.CategoryIntegrity,
					"tool call %q arguments must be a valid JSON object map, got %s", name, string(tc.Function.Arguments))
			}
			mappedToolCalls = append(mappedToolCalls, drivers.ToolCall{
				ID:        fmt.Sprintf("call_%d", i),
				Name:      name,
				Arguments: tc.Function.Arguments,
			})
		}
	}

	var usage drivers.TokenUsage
	if chatResp.PromptEvalCount != nil && chatResp.EvalCount != nil && *chatResp.PromptEvalCount >= 0 && *chatResp.EvalCount >= 0 {
		usage = drivers.TokenUsage{
			Input:  drivers.KnownMeasurement(*chatResp.PromptEvalCount),
			Cached: drivers.TokenMeasurement{}, // unknown
			Output: drivers.KnownMeasurement(*chatResp.EvalCount),
		}
	} else {
		usage = drivers.TokenUsage{} // unknown
	}

	return drivers.DirectAPIResponse{
		Content:   chatResp.Message.Content,
		ToolCalls: mappedToolCalls,
		Usage:     usage,
	}, nil
}

// Stream returns typed unsupported error as loopback streaming is not supported.
func (c *LoopbackClient) Stream(ctx context.Context, req drivers.DirectAPIRequest) (drivers.EventStream, error) {
	return nil, errs.New(errs.CategoryUnsupported, "streaming is not supported by loopback client")
}
