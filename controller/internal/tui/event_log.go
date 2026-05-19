package tui

import "dropcheck/controller/internal/watchstate"

func detailValue(value string) string {
	return watchstate.DetailValue(value)
}

func sanitizeLogText(value string) string {
	return watchstate.SanitizeLogText(value)
}
