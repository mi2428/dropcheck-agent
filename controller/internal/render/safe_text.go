package render

import (
	"fmt"
	"slices"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/safevalue"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func safePresentationText(value string) string                  { return safevalue.Text(value) }
func safePresentationField(label, value string) string          { return safevalue.Field(label, value) }
func safePresentationProto(message proto.Message) proto.Message { return safevalue.Proto(message) }

// Structured output omits whole-object string dumps, not typed numeric/IE evidence.
func structuredResult(result *controlpb.CommandResult) *controlpb.CommandResult {
	copy := SanitizedResult(result)
	var strip func(protoreflect.Message)
	strip = func(message protoreflect.Message) {
		message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
			if field.Kind() == protoreflect.StringKind && !field.IsList() {
				switch field.Name() {
				case "raw", "raw_link_properties", "raw_capabilities":
					message.Clear(field)
				}
			}
			if field.Kind() == protoreflect.MessageKind {
				if field.IsList() {
					for i := 0; i < value.List().Len(); i++ {
						strip(value.List().Get(i).Message())
					}
				} else {
					strip(value.Message())
				}
			}
			return true
		})
	}
	strip(copy.ProtoReflect())
	return copy
}

// Select literal identities on a detached observation before display normalization.
func presentationResult(result *controlpb.CommandResult, options command.Options) *controlpb.CommandResult {
	copy := proto.Clone(result).(*controlpb.CommandResult)
	selectScan := func(scan *controlpb.WifiScan, filter wifiMLOFilter) {
		if scan == nil || !filter.active() {
			return
		}
		if diagnosticFieldMap(scan.GetFields())["scan_result_total_count"] == "" {
			scan.Fields = append(scan.Fields, &controlpb.DiagnosticField{Key: "scan_result_total_count", Value: fmt.Sprint(len(scan.Results))})
		}
		scan.Results = filter.scanResults(scan.Results)
	}
	switch payload := copy.Payload.(type) {
	case *controlpb.CommandResult_WifiScan:
		selectScan(payload.WifiScan, wifiMLOFilter{ssid: options.WifiScanSSID, bssid: options.WifiScanBSSID})
	case *controlpb.CommandResult_WifiDiagnostics:
		if options.WifiRenderMode == command.WifiRenderModeEHT && payload.WifiDiagnostics != nil {
			diagnostics := payload.WifiDiagnostics
			filter := wifiMLOFilter{ssid: options.WifiEHTSSID, bssid: options.WifiEHTBSSID}
			selectScan(diagnostics.Scan, filter)
			if filter.active() {
				if status := diagnostics.Status; status != nil {
					status.Connection = filter.connection(status.Connection)
					if status.IpStatus != nil && !filter.matchesConnection(status.IpStatus.Wifi) {
						status.IpStatus = nil
					}
				}
				diagnostics.Networks = slices.DeleteFunc(diagnostics.Networks, func(network *controlpb.NetworkDiagnostics) bool {
					return !filter.matchesConnection(network.GetIpStatus().GetWifi())
				})
			}
		}
	}
	safevalue.SanitizeProto(copy)
	return copy
}
