package render

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/pipeline"
	"google.golang.org/protobuf/proto"
)

func TestPingPresentationUsesTypedPresenceNotNativeText(t *testing.T) {
	p := &controlpb.PingResult{Output: "3 packets transmitted, 3 received, 0% packet loss"}
	r := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_Ping{Ping: p}}
	text, err := CommandResult("agent", r, command.Options{})
	if err != nil || !slices.Equal(metadataQAValues(text, "Transmitted"), []string{"?"}) || !slices.Equal(metadataQAValues(text, "Received"), []string{"?"}) || !slices.Equal(metadataQAValues(text, "Packet loss (%)"), []string{"?"}) {
		t.Fatalf("unknown stats rendered as known: %s (%v)", text, err)
	}
	p.Transmitted, p.Received, p.PacketLossPercent = proto.Uint32(3), proto.Uint32(0), proto.Float64(100)
	text, err = CommandResult("agent", r, command.Options{})
	if err != nil || !slices.Equal(metadataQAValues(text, "Transmitted"), []string{"3"}) || !slices.Equal(metadataQAValues(text, "Received"), []string{"0"}) || !slices.Equal(metadataQAValues(text, "Packet loss (%)"), []string{"100"}) {
		t.Fatalf("known zero was lost: %s (%v)", text, err)
	}
	jsonText, err := pipeline.JSON(text, true)
	if err != nil {
		t.Fatal(err)
	}
	var data struct {
		OK    bool     `json:"ok"`
		Lines []string `json:"lines"`
	}
	if err := json.Unmarshal([]byte(jsonText), &data); err != nil || !data.OK || !slices.Equal(metadataQAValues(strings.Join(data.Lines, "\n"), "Received"), []string{"0"}) || !slices.Equal(metadataQAValues(strings.Join(data.Lines, "\n"), "Min/avg/max (ms)"), []string{"?/?/?"}) {
		t.Fatalf("JSON presence mismatch: %s (%v)", jsonText, err)
	}
	safe := SanitizedResult(r).GetPing()
	if safe.Received == nil || *safe.Received != 0 || safe.MinMs != nil {
		t.Fatal("safe snapshot changed typed presence")
	}
}
