// Package linuxcli parses the non-interactive dropcheck command form.
//
// PC host flags are extracted only before the first network-command token.
// The remaining argv tokens go directly to command.ParseTokens unchanged.
package linuxcli
