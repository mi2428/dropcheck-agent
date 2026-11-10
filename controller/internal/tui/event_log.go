package tui

import "github.com/mi2428/dropcheck-agent/controller/internal/tuistate"

func detailValue(value string) string {
	return tuistate.DetailValue(value)
}

func sanitizeLogText(value string) string {
	return tuistate.SanitizeLogText(value)
}
