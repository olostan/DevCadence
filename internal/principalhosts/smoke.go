package principalhosts

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// SmokeTimeout bounds the basic connectivity smoke.
const SmokeTimeout = 30 * time.Second

// SmokeProjectState launches the planned no-argument devcadence-mcp exactly as
// a host would (same executable, same env keys, an empty source-free working
// directory), initializes over newline-delimited stdio JSON-RPC and calls the
// read-only project_state tool once. It speaks the identical wire contract and
// links no MCP SDK. Only pass/fail metadata is returned, never state content.
// It proves launch, binding and a read; it is not task or review readiness.
func SmokeProjectState(ctx context.Context, req IntegrationRequest) ProbeCheck {
	fail := func(ref string) ProbeCheck {
		return ProbeCheck{Route: "mcp_connection", Result: ResultFail, EvidenceRef: ref}
	}
	ctx, cancel := context.WithTimeout(ctx, SmokeTimeout)
	defer cancel()
	cwd, err := os.MkdirTemp("", "dc-smoke-")
	if err != nil {
		return ProbeCheck{Route: "mcp_connection", Result: ResultUnavailable, EvidenceRef: "smoke:no-workdir"}
	}
	defer os.RemoveAll(cwd)
	cmd := exec.CommandContext(ctx, req.MCPExecutable)
	cmd.Dir = cwd
	cmd.Env = []string{"DEVCADENCE_PROJECT_ID=" + req.ProjectID, "DEVCADENCE_HOME=" + req.RuntimeHome}
	if req.PrincipalBindingPath != "" {
		cmd.Env = append(cmd.Env, "DEVCADENCE_PRINCIPAL_BINDING="+req.PrincipalBindingPath)
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		return fail("smoke:pipe")
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return fail("smoke:pipe")
	}
	if err := cmd.Start(); err != nil {
		return fail("smoke:launch-failed")
	}
	defer func() { in.Close(); cancel(); _ = cmd.Wait() }()

	lines := make(chan map[string]any, 8)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
		for sc.Scan() {
			var m map[string]any
			if json.Unmarshal(sc.Bytes(), &m) == nil {
				lines <- m
			}
		}
	}()
	send := func(m map[string]any) error {
		b, _ := json.Marshal(m)
		_, err := in.Write(append(b, '\n'))
		return err
	}
	await := func(id float64) (map[string]any, bool) {
		for {
			select {
			case m, ok := <-lines:
				if !ok {
					return nil, false
				}
				if m["id"] == id {
					return m, true
				}
			case <-ctx.Done():
				return nil, false
			}
		}
	}
	if send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{
		"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "devcadence-host-smoke", "version": "1"}}}) != nil {
		return fail("smoke:write-failed")
	}
	if m, ok := await(1); !ok || m["error"] != nil {
		return fail("smoke:initialize-failed")
	}
	_ = send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	if send(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{
		"name": "project_state", "arguments": map[string]any{"meta": map[string]any{
			"schema_version": "1.0", "project_id": req.ProjectID, "correlation_id": "host-smoke"}}}}) != nil {
		return fail("smoke:write-failed")
	}
	m, ok := await(2)
	if !ok {
		return ProbeCheck{Route: "mcp_connection", Result: ResultUnavailable, EvidenceRef: "smoke:no-response"}
	}
	res, _ := m["result"].(map[string]any)
	sc, _ := res["structuredContent"].(map[string]any)
	if m["error"] != nil || res == nil || res["isError"] == true || sc["state_revision"] == nil {
		return fail("smoke:project-state-refused")
	}
	return ProbeCheck{Route: "mcp_connection", Result: ResultPass, EvidenceRef: fmt.Sprintf("smoke:project_state:%s", req.ProjectID)}
}
