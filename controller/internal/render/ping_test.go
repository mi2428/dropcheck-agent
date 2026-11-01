package render

import (
	"encoding/json"
	"strings"
	"testing"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/pipeline"
	"google.golang.org/protobuf/proto"
)

func TestPingPresentationUsesTypedPresenceNotNativeText(t *testing.T) {
	p := &controlpb.PingResult{Output: "3 packets transmitted, 3 received, 0% packet loss"}
	r := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_Ping{Ping: p}}
	text, err := CommandResult("agent", r, command.Options{}, pipeline.FormatText)
	if err != nil || !strings.Contains(text, "transmitted=? received=? loss=?%") {
		t.Fatalf("unknown stats rendered as known: %s (%v)", text, err)
	}
	p.Transmitted, p.Received, p.PacketLossPercent = proto.Uint32(3), proto.Uint32(0), proto.Float64(100)
	text, err = CommandResult("agent", r, command.Options{}, pipeline.FormatText)
	if err != nil || !strings.Contains(text, "transmitted=3 received=0 loss=100%") {
		t.Fatalf("known zero was lost: %s (%v)", text, err)
	}
	jsonText, err := CommandResult("agent", r, command.Options{}, pipeline.FormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	var data struct {
		Ping map[string]json.RawMessage `json:"ping"`
	}
	if err := json.Unmarshal([]byte(jsonText), &data); err != nil || string(data.Ping["received"]) != "0" || data.Ping["min_ms"] != nil {
		t.Fatalf("JSON presence mismatch: %s (%v)", jsonText, err)
	}
}
