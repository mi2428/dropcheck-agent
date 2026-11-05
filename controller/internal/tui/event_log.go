package tui

import "github.com/mi2428/dropcheck-agent/controller/internal/watchstate"

func detailValue(value string) string {
	return watchstate.DetailValue(value)
}

func sanitizeLogText(value string) string {
	return watchstate.SanitizeLogText(value)
}
