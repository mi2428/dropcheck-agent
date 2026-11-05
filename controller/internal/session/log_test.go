package session

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mi2428/dropcheck-agent/controller/internal/control"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
)

func TestCustomLogHandlerReceivesAllLevelsAndDefaultFilters(t *testing.T) {
	var levels []controlpb.CommandLog_Level
	handler := sessionLogHandler(Options{OnLog: func(e control.LogEvent) { levels = append(levels, e.Level) }})
	var out bytes.Buffer
	for _, level := range []controlpb.CommandLog_Level{controlpb.CommandLog_LEVEL_UNSPECIFIED, controlpb.CommandLog_LEVEL_DEBUG, controlpb.CommandLog_LEVEL_INFO, controlpb.CommandLog_LEVEL_WARN, controlpb.CommandLog_LEVEL_ERROR, 99} {
		event := control.LogEvent{AgentID: "synthetic", Level: level, Message: level.String()}
		handler(event)
		writeSessionLog(&out, event)
	}
	if len(levels) != 6 || strings.Contains(out.String(), "LEVEL_DEBUG") || strings.Contains(out.String(), "LEVEL_INFO") || !strings.Contains(out.String(), "LEVEL_WARN") || !strings.Contains(out.String(), "LEVEL_ERROR") || !strings.Contains(out.String(), "LEVEL_UNSPECIFIED") || !strings.Contains(out.String(), "99") {
		t.Fatalf("log contract changed: %v %s", levels, out.String())
	}
}
