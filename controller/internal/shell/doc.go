// Package shell parses the Controller Shell language.
//
// The Shell tokenizes a line, splits quote-aware output pipelines, and passes
// network tokens to command.ParseShellTokens. Its show/debug/request/test/monitor
// hierarchy has no configuration mode or retired one-shot argv adapter.
package shell
