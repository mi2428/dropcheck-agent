// Package mcpserver exposes dropcheck controller operations through MCP.
//
// The package is intentionally built on the typed command.Operation boundary
// shared by the CLI and shell. MCP handlers construct operations directly and
// execute them through a Backend, keeping protocol handling separate from ADB,
// gRPC session ownership, and Android command dispatch.
//
// The server registers first-class tools for session lifecycle, agent
// discovery, Wi-Fi inspection and control, Android-side network probes,
// host-side adb diagnostics, standalone configuration and archives, and the
// higher-level dropcheck_run workflow. It also exposes session and agent
// resources, standalone resource templates, and prompts that guide common
// connectivity, EHT, and NOC smoke-check workflows.
//
// Tools that need Wi-Fi credentials accept either passphrase or passphrase_env.
// Prefer passphrase_env in MCP clients so secrets stay in the server process
// environment instead of tool-call transcripts. Long-running tools emit MCP
// progress and logging notifications when the client advertises support.
package mcpserver
