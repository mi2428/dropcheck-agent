// Package mcpserver exposes dropcheck controller operations as MCP tools.
//
// The package is intentionally built on the typed command.Operation boundary
// shared by the CLI and shell. MCP handlers construct operations directly and
// execute them through a Backend, keeping protocol handling separate from ADB,
// gRPC session ownership, and Android command dispatch.
package mcpserver
