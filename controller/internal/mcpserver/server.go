package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewServer builds the dropcheck MCP server with all supported tools.
func NewServer(backend Backend) *mcp.Server {
	if backend == nil {
		backend = NewRealBackend(SessionStartOptions{})
	}
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "dropcheck-mcp",
		Title:   "Dropcheck MCP",
		Version: "0.1.0",
	}, nil)
	registerTools(server, backend)
	return server
}

// RunStdio runs a dropcheck MCP server on stdin/stdout.
func RunStdio(ctx context.Context, backend Backend) error {
	server := NewServer(backend)
	return server.Run(ctx, &mcp.StdioTransport{})
}
