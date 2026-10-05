package mcpadapter

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/olostan/DevCadence/internal/principal"
)

// ToolResultForTest exposes the response normaliser to the black-box tests.
func ToolResultForTest(response any, isError bool) *mcp.CallToolResult {
	return toolResult(response, isError)
}

// DispatchForTest runs one call through the panic and size guards.
func DispatchForTest(call func(context.Context, principal.CallerContext, []byte) (any, *principal.SemanticError)) *mcp.CallToolResult {
	return dispatch(context.Background(), principal.CallerContext{}, "test", call, nil)
}

// RepositoryObserverForTest exposes the launch-time observer selection.
var RepositoryObserverForTest = repositoryObserver
