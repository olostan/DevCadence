// Command devcadence-mcp is the no-argument stdio MCP server of the semantic
// Principal interface (WP-M5-2). It takes its project, home and protected
// caller binding from the environment, never from the working directory, and
// writes only protocol frames to stdout.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/olostan/DevCadence/internal/mcpadapter"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	code := mcpadapter.LaunchWith(ctx, os.Args[1:], os.Getenv, &mcp.StdioTransport{}, os.Stderr, selfhostTasks)
	stop()
	os.Exit(code)
}
