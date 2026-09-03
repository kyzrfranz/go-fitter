// Package mcp exposes the activities and health data as MCP tools so an
// LLM can pull the same views the REST API serves without bouncing through
// HTTP.
package mcp

import (
	"net/http"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	v1 "github.com/kyzrfranz/go-fitter/api/v1"
	"github.com/kyzrfranz/go-fitter/internal/db"
	restActivity "github.com/kyzrfranz/go-fitter/internal/rest/activity"
)

type Deps struct {
	Activities db.DatabaseClient[v1.Activity]
	Health     db.DatabaseClient[db.HealthMetric]
	Retriever  restActivity.RawReader
}

// NewHandler wires the MCP tool surface onto a Streamable HTTP handler. Mount
// it on a single route (e.g. /mcp) and the server speaks the protocol on both
// GET (SSE) and POST (JSON-RPC).
func NewHandler(deps Deps) http.Handler {
	s := server.NewMCPServer(
		"go-fitter",
		"1.0.0",
		server.WithToolCapabilities(false),
	)

	registerActivityTools(s, deps)
	registerSeriesTool(s, deps)
	registerHealthTool(s, deps)
	registerDescribeTool(s, deps)

	return server.NewStreamableHTTPServer(s, server.WithStateLess(true))
}

// jsonResult marshals v and wraps it in a text tool result. Errors are
// converted to mcp tool-result errors so the LLM sees them inline.
func jsonResult(v any, marshal func(any) ([]byte, error)) (*mcp.CallToolResult, error) {
	b, err := marshal(v)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("marshal failed", err), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}