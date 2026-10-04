// Package shell parses the Controller Shell language.
//
// The Shell tokenizes a line, splits quote-aware output pipelines, and passes
// network tokens to command.ParseTokens. It has one flat operational mode.
package shell
