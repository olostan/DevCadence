package sessionclients_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/olostan/DevCadence/internal/cognition/drivers"
	"github.com/olostan/DevCadence/internal/cognition/sessionclients"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/execpolicy"
)

func sampleLimits() execpolicy.ExecutionLimits {
	return execpolicy.ExecutionLimits{
		MaxTurns:               10,
		MaxToolCalls:           20,
		MaxTotalTokens:         100000,
		MaxDurationSeconds:     30,
		MaxOutputTokensPerCall: 2048,
		MaxRequestBytes:        65536,
		MaxAPISpendMicroUSD:    500000,
	}
}

func TestNewLoopbackClient_Validation(t *testing.T) {
	t.Run("valid loopback addresses", func(t *testing.T) {
		validURLs := []string{
			"http://127.0.0.1:11434",
			"http://127.0.0.1:8080",
			"http://[::1]:11434",
			"http://localhost:11434",
			"", // defaults to 127.0.0.1:11434
		}
		for _, u := range validURLs {
			client, err := sessionclients.NewLoopbackClient(u, "test-model", sampleLimits(), nil)
			if err != nil {
				t.Fatalf("expected valid client for %q, got err: %v", u, err)
			}
			if client == nil {
				t.Fatalf("expected non-nil client for %q", u)
			}
		}
	})

	t.Run("rejects non-loopback hosts", func(t *testing.T) {
		invalidURLs := []string{
			"http://example.com:11434",
			"http://192.168.1.1:11434",
			"http://10.0.0.1:11434",
			"http://8.8.8.8:80",
			"http://169.254.169.254:80",
		}
		for _, u := range invalidURLs {
			_, err := sessionclients.NewLoopbackClient(u, "test-model", sampleLimits(), nil)
			if err == nil {
				t.Fatalf("expected error for non-loopback URL %q, got nil", u)
			}
			if !errors.Is(err, errs.ErrPolicyDenied) {
				t.Errorf("expected ErrPolicyDenied for %q, got %v", u, err)
			}
		}
	})

	t.Run("rejects non-http scheme", func(t *testing.T) {
		_, err := sessionclients.NewLoopbackClient("https://127.0.0.1:11434", "test-model", sampleLimits(), nil)
		if err == nil || !errors.Is(err, errs.ErrPolicyDenied) {
			t.Errorf("expected ErrPolicyDenied for https, got %v", err)
		}
	})

	t.Run("rejects credentials in URL", func(t *testing.T) {
		_, err := sessionclients.NewLoopbackClient("http://user:pass@127.0.0.1:11434", "test-model", sampleLimits(), nil)
		if err == nil || !errors.Is(err, errs.ErrPolicyDenied) {
			t.Errorf("expected ErrPolicyDenied for credentials in URL, got %v", err)
		}
	})

	t.Run("rejects custom client with proxy", func(t *testing.T) {
		proxyURL, _ := url.Parse("http://127.0.0.1:8888")
		httpClient := &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyURL(proxyURL),
			},
		}
		_, err := sessionclients.NewLoopbackClient("http://127.0.0.1:11434", "test-model", sampleLimits(), httpClient)
		if err == nil || !errors.Is(err, errs.ErrPolicyDenied) {
			t.Errorf("expected ErrPolicyDenied for proxy configuration, got %v", err)
		}
	})
}

func TestLoopbackClient_Complete_ProtocolAndFormat(t *testing.T) {
	var capturedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/chat" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Content-Type") != "application/json" {
			http.Error(w, "invalid content-type", http.StatusBadRequest)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}

		resp := map[string]any{
			"model": "test-model",
			"message": map[string]any{
				"role":    "assistant",
				"content": "Hello from loopback!",
			},
			"done":              true,
			"prompt_eval_count": 12,
			"eval_count":        34,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	limits := sampleLimits()
	limits.MaxOutputTokensPerCall = 1024
	client, err := sessionclients.NewLoopbackClient(server.URL, "test-model", limits, nil)
	if err != nil {
		t.Fatalf("unexpected NewLoopbackClient error: %v", err)
	}

	req := drivers.DirectAPIRequest{
		ModelID:      "test-model",
		SystemPrompt: "Be a helpful assistant",
		Messages: []drivers.DirectMessage{
			{Role: "user", Content: "Hi"},
		},
		Tools: []drivers.ToolDefinition{
			{
				Name:        "get_weather",
				Description: "fetch weather",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`),
			},
		},
	}

	resp, err := client.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected Complete error: %v", err)
	}

	if resp.Content != "Hello from loopback!" {
		t.Errorf("Content = %q, want 'Hello from loopback!'", resp.Content)
	}

	// Verify captured body fields
	if capturedBody["model"] != "test-model" {
		t.Errorf("model = %v, want 'test-model'", capturedBody["model"])
	}
	if capturedBody["stream"] != false {
		t.Errorf("stream = %v, want false", capturedBody["stream"])
	}
	options, ok := capturedBody["options"].(map[string]any)
	if !ok {
		t.Fatalf("options missing or invalid: %v", capturedBody["options"])
	}
	if int64(options["num_predict"].(float64)) != 1024 {
		t.Errorf("num_predict = %v, want 1024", options["num_predict"])
	}

	// Verify messages: system prompt + user message
	messages, ok := capturedBody["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %v", messages)
	}
	sysMsg := messages[0].(map[string]any)
	if sysMsg["role"] != "system" || sysMsg["content"] != "Be a helpful assistant" {
		t.Errorf("system message = %v", sysMsg)
	}
	userMsg := messages[1].(map[string]any)
	if userMsg["role"] != "user" || userMsg["content"] != "Hi" {
		t.Errorf("user message = %v", userMsg)
	}

	// Verify tools: get_weather
	tools, ok := capturedBody["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %v", tools)
	}
	tool0 := tools[0].(map[string]any)
	if tool0["type"] != "function" {
		t.Errorf("tool type = %v, want 'function'", tool0["type"])
	}
	fn := tool0["function"].(map[string]any)
	if fn["name"] != "get_weather" {
		t.Errorf("fn name = %v, want 'get_weather'", fn["name"])
	}
}

func TestLoopbackClient_Complete_TokenUsageKnownness(t *testing.T) {
	testCases := []struct {
		name             string
		promptEvalCount  *int64
		evalCount        *int64
		wantInputKnown   bool
		wantInputValue   int64
		wantCachedKnown  bool
		wantOutputKnown  bool
		wantOutputValue  int64
		wantTotalKnown   bool
		wantTotalValue   int64
		wantCompleteFlag bool
	}{
		{
			name:             "both counts present and positive",
			promptEvalCount:  ptrInt64(25),
			evalCount:        ptrInt64(75),
			wantInputKnown:   true,
			wantInputValue:   25,
			wantCachedKnown:  false,
			wantOutputKnown:  true,
			wantOutputValue:  75,
			wantTotalKnown:   true,
			wantTotalValue:   100,
			wantCompleteFlag: false,
		},
		{
			name:             "both counts zero",
			promptEvalCount:  ptrInt64(0),
			evalCount:        ptrInt64(0),
			wantInputKnown:   true,
			wantInputValue:   0,
			wantCachedKnown:  false,
			wantOutputKnown:  true,
			wantOutputValue:  0,
			wantTotalKnown:   true,
			wantTotalValue:   0,
			wantCompleteFlag: false,
		},
		{
			name:             "missing prompt_eval_count (nil)",
			promptEvalCount:  nil,
			evalCount:        ptrInt64(50),
			wantInputKnown:   false,
			wantCachedKnown:  false,
			wantOutputKnown:  false,
			wantTotalKnown:   false,
			wantCompleteFlag: false,
		},
		{
			name:             "missing eval_count (nil)",
			promptEvalCount:  ptrInt64(50),
			evalCount:        nil,
			wantInputKnown:   false,
			wantCachedKnown:  false,
			wantOutputKnown:  false,
			wantTotalKnown:   false,
			wantCompleteFlag: false,
		},
		{
			name:             "negative prompt_eval_count",
			promptEvalCount:  ptrInt64(-1),
			evalCount:        ptrInt64(50),
			wantInputKnown:   false,
			wantCachedKnown:  false,
			wantOutputKnown:  false,
			wantTotalKnown:   false,
			wantCompleteFlag: false,
		},
		{
			name:             "negative eval_count",
			promptEvalCount:  ptrInt64(50),
			evalCount:        ptrInt64(-5),
			wantInputKnown:   false,
			wantCachedKnown:  false,
			wantOutputKnown:  false,
			wantTotalKnown:   false,
			wantCompleteFlag: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				resp := map[string]any{
					"model": "m",
					"message": map[string]any{
						"role":    "assistant",
						"content": "ok",
					},
					"done": true,
				}
				if tc.promptEvalCount != nil {
					resp["prompt_eval_count"] = *tc.promptEvalCount
				}
				if tc.evalCount != nil {
					resp["eval_count"] = *tc.evalCount
				}
				_ = json.NewEncoder(w).Encode(resp)
			}))
			defer server.Close()

			client, err := sessionclients.NewLoopbackClient(server.URL, "m", sampleLimits(), nil)
			if err != nil {
				t.Fatalf("unexpected NewLoopbackClient error: %v", err)
			}

			resp, err := client.Complete(context.Background(), drivers.DirectAPIRequest{
				ModelID: "m",
				Prompt:  "hello",
			})
			if err != nil {
				t.Fatalf("unexpected Complete error: %v", err)
			}

			if resp.Usage.Input.Known != tc.wantInputKnown {
				t.Errorf("Input.Known = %v, want %v", resp.Usage.Input.Known, tc.wantInputKnown)
			}
			if tc.wantInputKnown && resp.Usage.Input.Value != tc.wantInputValue {
				t.Errorf("Input.Value = %d, want %d", resp.Usage.Input.Value, tc.wantInputValue)
			}
			if resp.Usage.Cached.Known != tc.wantCachedKnown {
				t.Errorf("Cached.Known = %v, want %v", resp.Usage.Cached.Known, tc.wantCachedKnown)
			}
			if resp.Usage.Output.Known != tc.wantOutputKnown {
				t.Errorf("Output.Known = %v, want %v", resp.Usage.Output.Known, tc.wantOutputKnown)
			}
			if tc.wantOutputKnown && resp.Usage.Output.Value != tc.wantOutputValue {
				t.Errorf("Output.Value = %d, want %d", resp.Usage.Output.Value, tc.wantOutputValue)
			}

			tot, known := resp.Usage.Total()
			if known != tc.wantTotalKnown {
				t.Errorf("Total() known = %v, want %v", known, tc.wantTotalKnown)
			}
			if tc.wantTotalKnown && tot != tc.wantTotalValue {
				t.Errorf("Total() value = %d, want %d", tot, tc.wantTotalValue)
			}
			if resp.Usage.Complete() != tc.wantCompleteFlag {
				t.Errorf("Complete() = %v, want %v", resp.Usage.Complete(), tc.wantCompleteFlag)
			}
		})
	}
}

func TestLoopbackClient_Complete_ToolValidation(t *testing.T) {
	t.Run("valid tool call with json object arguments", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := map[string]any{
				"model": "m",
				"message": map[string]any{
					"role": "assistant",
					"tool_calls": []map[string]any{
						{
							"function": map[string]any{
								"name": "calc",
								"arguments": map[string]any{
									"x": 10,
									"y": 20,
								},
							},
						},
					},
				},
				"done":              true,
				"prompt_eval_count": 5,
				"eval_count":        5,
			}
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := sessionclients.NewLoopbackClient(server.URL, "m", sampleLimits(), nil)
		if err != nil {
			t.Fatalf("unexpected NewLoopbackClient error: %v", err)
		}

		resp, err := client.Complete(context.Background(), drivers.DirectAPIRequest{
			ModelID: "m",
			Tools: []drivers.ToolDefinition{
				{Name: "calc"},
			},
		})
		if err != nil {
			t.Fatalf("unexpected Complete error: %v", err)
		}
		if len(resp.ToolCalls) != 1 {
			t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
		}
		if resp.ToolCalls[0].Name != "calc" {
			t.Errorf("tool name = %q, want 'calc'", resp.ToolCalls[0].Name)
		}
		var args map[string]any
		if err := json.Unmarshal(resp.ToolCalls[0].Arguments, &args); err != nil {
			t.Fatalf("arguments not valid json: %v", err)
		}
		if int(args["x"].(float64)) != 10 || int(args["y"].(float64)) != 20 {
			t.Errorf("unexpected args content: %v", args)
		}
	})

	t.Run("rejects tool call with unrequested tool name", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := map[string]any{
				"model": "m",
				"message": map[string]any{
					"role": "assistant",
					"tool_calls": []map[string]any{
						{
							"function": map[string]any{
								"name":      "unauthorized_tool",
								"arguments": map[string]any{},
							},
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := sessionclients.NewLoopbackClient(server.URL, "m", sampleLimits(), nil)
		if err != nil {
			t.Fatalf("unexpected NewLoopbackClient error: %v", err)
		}

		_, err = client.Complete(context.Background(), drivers.DirectAPIRequest{
			ModelID: "m",
			Tools: []drivers.ToolDefinition{
				{Name: "calc"},
			},
		})
		if err == nil || !errors.Is(err, errs.ErrIntegrity) {
			t.Errorf("expected ErrIntegrity for unrequested tool, got %v", err)
		}
	})

	t.Run("rejects tool call with non-object arguments (array)", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := map[string]any{
				"model": "m",
				"message": map[string]any{
					"role": "assistant",
					"tool_calls": []map[string]any{
						{
							"function": map[string]any{
								"name":      "calc",
								"arguments": []any{1, 2, 3},
							},
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := sessionclients.NewLoopbackClient(server.URL, "m", sampleLimits(), nil)
		if err != nil {
			t.Fatalf("unexpected NewLoopbackClient error: %v", err)
		}

		_, err = client.Complete(context.Background(), drivers.DirectAPIRequest{
			ModelID: "m",
			Tools: []drivers.ToolDefinition{
				{Name: "calc"},
			},
		})
		if err == nil || !errors.Is(err, errs.ErrIntegrity) {
			t.Errorf("expected ErrIntegrity for array arguments, got %v", err)
		}
	})

	t.Run("rejects tool call with non-object arguments (string)", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := map[string]any{
				"model": "m",
				"message": map[string]any{
					"role": "assistant",
					"tool_calls": []map[string]any{
						{
							"function": map[string]any{
								"name":      "calc",
								"arguments": "some string",
							},
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := sessionclients.NewLoopbackClient(server.URL, "m", sampleLimits(), nil)
		if err != nil {
			t.Fatalf("unexpected NewLoopbackClient error: %v", err)
		}

		_, err = client.Complete(context.Background(), drivers.DirectAPIRequest{
			ModelID: "m",
			Tools: []drivers.ToolDefinition{
				{Name: "calc"},
			},
		})
		if err == nil || !errors.Is(err, errs.ErrIntegrity) {
			t.Errorf("expected ErrIntegrity for string arguments, got %v", err)
		}
	})

	t.Run("rejects tool call with non-object arguments (null)", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := map[string]any{
				"model": "m",
				"message": map[string]any{
					"role": "assistant",
					"tool_calls": []map[string]any{
						{
							"function": map[string]any{
								"name":      "calc",
								"arguments": nil,
							},
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer server.Close()

		client, err := sessionclients.NewLoopbackClient(server.URL, "m", sampleLimits(), nil)
		if err != nil {
			t.Fatalf("unexpected NewLoopbackClient error: %v", err)
		}

		_, err = client.Complete(context.Background(), drivers.DirectAPIRequest{
			ModelID: "m",
			Tools: []drivers.ToolDefinition{
				{Name: "calc"},
			},
		})
		if err == nil || !errors.Is(err, errs.ErrIntegrity) {
			t.Errorf("expected ErrIntegrity for null arguments, got %v", err)
		}
	})
}

func TestLoopbackClient_Complete_SizeLimits(t *testing.T) {
	t.Run("rejects request larger than MaxRequestBytes before send", func(t *testing.T) {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			_ = json.NewEncoder(w).Encode(map[string]any{"done": true})
		}))
		defer server.Close()

		limits := sampleLimits()
		limits.MaxRequestBytes = 200 // very small limit

		client, err := sessionclients.NewLoopbackClient(server.URL, "m", limits, nil)
		if err != nil {
			t.Fatalf("unexpected NewLoopbackClient error: %v", err)
		}

		// Prompt of 500 characters exceeds 200 bytes limit
		bigPrompt := strings.Repeat("A", 500)
		_, err = client.Complete(context.Background(), drivers.DirectAPIRequest{
			ModelID: "m",
			Prompt:  bigPrompt,
		})
		if err == nil || !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument for oversize request, got %v", err)
		}
		if calls != 0 {
			t.Errorf("expected 0 HTTP calls due to preflight refusal, got %d", calls)
		}
	})

	t.Run("rejects response larger than 4 MiB limit", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Write 4.5 MiB of data
			data := make([]byte, 4500000)
			for i := range data {
				data[i] = ' '
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(data)
		}))
		defer server.Close()

		client, err := sessionclients.NewLoopbackClient(server.URL, "m", sampleLimits(), nil)
		if err != nil {
			t.Fatalf("unexpected NewLoopbackClient error: %v", err)
		}

		_, err = client.Complete(context.Background(), drivers.DirectAPIRequest{
			ModelID: "m",
			Prompt:  "hi",
		})
		if err == nil || !errors.Is(err, errs.ErrProbeFailed) {
			t.Errorf("expected ErrProbeFailed for oversize response, got %v", err)
		}
	})
}

func TestLoopbackClient_Complete_FailsOnRedirect(t *testing.T) {
	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1:11434/redirected", http.StatusFound)
	}))
	defer redirectServer.Close()

	client, err := sessionclients.NewLoopbackClient(redirectServer.URL, "m", sampleLimits(), nil)
	if err != nil {
		t.Fatalf("unexpected NewLoopbackClient error: %v", err)
	}

	_, err = client.Complete(context.Background(), drivers.DirectAPIRequest{
		ModelID: "m",
		Prompt:  "hi",
	})
	if err == nil {
		t.Fatal("expected error on redirect, got nil")
	}
	if !strings.Contains(err.Error(), "redirects are forbidden") {
		t.Errorf("expected redirects are forbidden error, got %v", err)
	}
}

func TestLoopbackClient_Stream_Unsupported(t *testing.T) {
	client, err := sessionclients.NewLoopbackClient("http://127.0.0.1:11434", "m", sampleLimits(), nil)
	if err != nil {
		t.Fatalf("unexpected NewLoopbackClient error: %v", err)
	}

	stream, err := client.Stream(context.Background(), drivers.DirectAPIRequest{})
	if err == nil || !errors.Is(err, errs.ErrUnsupported) {
		t.Errorf("expected ErrUnsupported, got %v", err)
	}
	if stream != nil {
		t.Errorf("expected nil stream, got %v", stream)
	}
}

func TestLoopbackClient_Complete_Errors(t *testing.T) {
	t.Run("server returns 500 error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}))
		defer server.Close()

		client, err := sessionclients.NewLoopbackClient(server.URL, "m", sampleLimits(), nil)
		if err != nil {
			t.Fatalf("unexpected NewLoopbackClient error: %v", err)
		}

		_, err = client.Complete(context.Background(), drivers.DirectAPIRequest{ModelID: "m", Prompt: "hi"})
		if err == nil || !errors.Is(err, errs.ErrModelUnavailable) {
			t.Errorf("expected ErrModelUnavailable for 500 status, got %v", err)
		}
	})

	t.Run("server returns invalid JSON", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("not-valid-json!"))
		}))
		defer server.Close()

		client, err := sessionclients.NewLoopbackClient(server.URL, "m", sampleLimits(), nil)
		if err != nil {
			t.Fatalf("unexpected NewLoopbackClient error: %v", err)
		}

		_, err = client.Complete(context.Background(), drivers.DirectAPIRequest{ModelID: "m", Prompt: "hi"})
		if err == nil || !errors.Is(err, errs.ErrProbeFailed) {
			t.Errorf("expected ErrProbeFailed for invalid JSON, got %v", err)
		}
	})

	t.Run("context cancelled", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"done": true})
		}))
		defer server.Close()

		client, err := sessionclients.NewLoopbackClient(server.URL, "m", sampleLimits(), nil)
		if err != nil {
			t.Fatalf("unexpected NewLoopbackClient error: %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // immediately cancel
		_, err = client.Complete(ctx, drivers.DirectAPIRequest{ModelID: "m", Prompt: "hi"})
		if err == nil {
			t.Fatal("expected error for cancelled context, got nil")
		}
	})

	t.Run("invalid URLs", func(t *testing.T) {
		_, err := sessionclients.NewLoopbackClient("://invalid-url", "m", sampleLimits(), nil)
		if err == nil || !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument for malformed URL, got %v", err)
		}

		_, err = sessionclients.NewLoopbackClient("http://", "m", sampleLimits(), nil)
		if err == nil || !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument for empty host URL, got %v", err)
		}
	})

	t.Run("connection error on closed loopback port", func(t *testing.T) {
		client, err := sessionclients.NewLoopbackClient("http://127.0.0.1:54321", "m", sampleLimits(), nil)
		if err != nil {
			t.Fatalf("unexpected NewLoopbackClient error: %v", err)
		}
		_, err = client.Complete(context.Background(), drivers.DirectAPIRequest{ModelID: "m", Prompt: "hi"})
		if err == nil || !errors.Is(err, errs.ErrModelUnavailable) {
			t.Errorf("expected ErrModelUnavailable for connection failure, got %v", err)
		}
	})
}

func ptrInt64(v int64) *int64 {
	return &v
}

func TestLoopbackClient_Complete_ModelDivergenceRejected(t *testing.T) {
	httpCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpCalls++
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "bound-model",
			"message": map[string]any{
				"role":    "assistant",
				"content": "ok",
			},
			"done": true,
		})
	}))
	defer server.Close()

	client, err := sessionclients.NewLoopbackClient(server.URL, "bound-model", sampleLimits(), nil)
	if err != nil {
		t.Fatalf("NewLoopbackClient error: %v", err)
	}

	// 1. Divergent ModelID rejected before HTTP send.
	_, err = client.Complete(context.Background(), drivers.DirectAPIRequest{
		ModelID: "attacker-divergent-model",
		Prompt:  "test prompt",
	})
	if err == nil {
		t.Fatal("expected error for divergent model ID, got nil")
	}
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument, got %v", err)
	}
	if !strings.Contains(err.Error(), "diverges from bound model") {
		t.Errorf("expected error message mentioning divergence, got %v", err)
	}
	if httpCalls != 0 {
		t.Errorf("expected 0 HTTP requests before error, but server received %d calls", httpCalls)
	}

	// 2. Matching ModelID accepted.
	_, err = client.Complete(context.Background(), drivers.DirectAPIRequest{
		ModelID: "bound-model",
		Prompt:  "test prompt",
	})
	if err != nil {
		t.Fatalf("unexpected error for matching model ID: %v", err)
	}
	if httpCalls != 1 {
		t.Errorf("expected 1 HTTP request for matching model, got %d", httpCalls)
	}

	// 3. Empty ModelID inherits bound model.
	_, err = client.Complete(context.Background(), drivers.DirectAPIRequest{
		ModelID: "",
		Prompt:  "test prompt",
	})
	if err != nil {
		t.Fatalf("unexpected error for empty model ID: %v", err)
	}
	if httpCalls != 2 {
		t.Errorf("expected 2 HTTP requests total, got %d", httpCalls)
	}
}
