// Package pipeline parses and applies interactive shell output pipelines.
//
// Pipelines are intentionally presentation-layer filters. They can request JSON
// display, filter rendered text with regular expressions, or count output
// lines. They do not alter the command sent to an Android agent.
package pipeline
