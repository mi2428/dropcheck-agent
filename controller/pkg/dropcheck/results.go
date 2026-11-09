package dropcheck

import (
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/acquire"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
)

// Result is caller-owned acquisition data, not sanitized presentation output.
// A nil Raw means no terminal result was obtained. A nil Go error does not imply
// success: inspect Raw.Status and every Finding. Optional protobuf pointers
// distinguish absent observations from known zero/false; getters alone erase
// that distinction. Non-optional zero scalars need their observation metadata.
type Result struct {
	// Raw is the terminal acquisition (or merged compound view), without commands
	// or local display options. It can contain sensitive data from the agent.
	Raw *CommandResult
	// Parts preserves individual acquisitions in execution order, even on failure.
	// Raw and each Part.Raw are independent snapshots, not aliases of each other.
	Parts []Part
	// Findings reports controller-side checks, including inconsistent payloads.
	Findings []Finding
}

// Part records one attempted acquisition, not a scenario/tui step.
type Part struct {
	// AgentID and SessionID identify the pinned connection for this acquisition.
	AgentID, SessionID string
	// Name is the dotted operation name; CommandID is controller-generated.
	Name, CommandID string
	// Started and Ended are host wall-clock times surrounding transport execution.
	// Their difference is host elapsed time, not Raw.ElapsedMs (agent runtime).
	Started, Ended time.Time
	// Raw is the independent terminal payload, or nil if none was acquired.
	Raw *CommandResult
	// Error is transport error text; Run also returns the actual Go error.
	Error string
}

// Finding is one controller-side acquisition or hop expectation check.
type Finding struct {
	// Metric is the stable assertion name.
	Metric string
	// Observed and Expected describe evidence and constraint, not sanitized text.
	Observed, Expected string
	// Passed reports a satisfied check. False with Missing=false is an observed
	// failure; Missing=true is unavailable/inconsistent evidence, never known zero.
	Passed bool
	// Missing distinguishes unavailable evidence from an observed failure.
	Missing bool
	// Message gives additional diagnostic context.
	Message string
}

func publicResult(value acquire.Result) Result {
	result := Result{Raw: value.Raw}
	for _, part := range value.Parts {
		result.Parts = append(result.Parts, Part{AgentID: part.AgentID, SessionID: part.SessionID, Name: part.Name, CommandID: part.CommandID, Started: part.Started, Ended: part.Ended, Raw: part.Raw, Error: part.Error})
	}
	for _, finding := range value.Findings {
		result.Findings = append(result.Findings, Finding{Metric: finding.Metric, Observed: finding.Observed, Expected: finding.Expected, Passed: finding.Passed, Missing: finding.Missing, Message: finding.Message})
	}
	return result
}

// CommandResult is the typed terminal agent payload. Only result-domain protocol
// aliases are exposed here, not commands, sessions, hello frames or transports.
// ElapsedMs is agent-reported milliseconds. Payload uses one of the named
// *Payload wrappers below; nil means no typed payload, not a successful empty one.
type CommandResult = controlpb.CommandResult

// Status is the agent-reported terminal acquisition status.
type Status = controlpb.CommandResult_Status

const (
	// StatusUnspecified is not a successful result.
	StatusUnspecified = controlpb.CommandResult_STATUS_UNSPECIFIED
	// StatusOK means the agent reported success; Findings can still reject evidence.
	StatusOK = controlpb.CommandResult_STATUS_OK
	// StatusFailed means the agent reported failure.
	StatusFailed = controlpb.CommandResult_STATUS_FAILED
	// StatusCanceled means the agent canceled the operation.
	StatusCanceled = controlpb.CommandResult_STATUS_CANCELED
)

// IPFamily identifies a result address family.
type IPFamily = controlpb.IpFamily

const (
	// IPFamilyUnspecified is unknown/agent-selected.
	IPFamilyUnspecified = controlpb.IpFamily_IP_FAMILY_UNSPECIFIED
	// IPFamilyAll requests or describes both families.
	IPFamilyAll = controlpb.IpFamily_IP_FAMILY_ALL
	// IPFamilyV4 identifies IPv4.
	IPFamilyV4 = controlpb.IpFamily_IP_FAMILY_IPV4
	// IPFamilyV6 identifies IPv6.
	IPFamilyV6 = controlpb.IpFamily_IP_FAMILY_IPV6
)

// DNSRecordType identifies a typed DNS answer.
type DNSRecordType = controlpb.DnsRecordType

const (
	// DNSRecordUnspecified is an unknown record type.
	DNSRecordUnspecified = controlpb.DnsRecordType_DNS_RECORD_TYPE_UNSPECIFIED
	// DNSRecordA identifies an IPv4 address record.
	DNSRecordA = controlpb.DnsRecordType_DNS_RECORD_TYPE_A
	// DNSRecordAAAA identifies an IPv6 address record.
	DNSRecordAAAA = controlpb.DnsRecordType_DNS_RECORD_TYPE_AAAA
)

// WiFiStatusData is acquired Wi-Fi state; inspect ObservationFields for availability.
type WiFiStatusData = controlpb.WifiStatus

// WiFiConnection is acquired association data, not proof of continuous association.
type WiFiConnection = controlpb.WifiConnection

// IPStatusData is acquired network/address state, with ObservationFields metadata.
type IPStatusData = controlpb.IpStatus

// WiFiDiagnosticsData is the compound Wi-Fi diagnostic payload.
type WiFiDiagnosticsData = controlpb.WifiDiagnostics

// WiFiCapabilitiesData contains supported/unsupported capabilities and errors.
type WiFiCapabilitiesData = controlpb.WifiCapabilities

// NetworkDiagnostics pairs an Android Network ID and its acquired IP state.
type NetworkDiagnostics = controlpb.NetworkDiagnostics

// WiFiScanData contains scan observations and acquisition errors.
type WiFiScanData = controlpb.WifiScan

// WiFiScanResult is one acquired access point; optional ObservationAgeMs preserves absence.
type WiFiScanResult = controlpb.WifiScanResult

// MLOLinkInfo is one observed multi-link association entry.
type MLOLinkInfo = controlpb.MloLinkInfo

// WiFiInformationElement contains acquired information-element bytes.
type WiFiInformationElement = controlpb.WifiInformationElement

// DiagnosticField is acquired key/value evidence, including observation availability.
type DiagnosticField = controlpb.DiagnosticField

// DiagnosticCheck is an agent-side assertion outcome.
type DiagnosticCheck = controlpb.DiagnosticCheck

// ConnectWiFiResult is the agent's connection outcome and IP evidence.
type ConnectWiFiResult = controlpb.ConnectWifiResult

// WiFiOperationResult is a disconnect/reconnect/forget outcome.
type WiFiOperationResult = controlpb.WifiOperationResult

// WiFiAssertResult is acquired wait/assert evidence.
type WiFiAssertResult = controlpb.WifiAssertResult

// WiFiEvent is an acquired monitor event with agent-provided time fields.
type WiFiEvent = controlpb.WifiEvent

// WiFiMonitorResult contains acquired monitor events and errors.
type WiFiMonitorResult = controlpb.WifiMonitorResult

// WiFiScanDetailData contains detailed SSID/BSSID scan evidence.
type WiFiScanDetailData = controlpb.WifiScanDetail

// WiFiCycleStep records a connection/probe/disconnection cycle outcome.
type WiFiCycleStep = controlpb.WifiCycleStep

// WiFiCycleResult contains repeated cycle outcomes and errors.
type WiFiCycleResult = controlpb.WifiCycleResult

// PingResult preserves optional numeric observations: nil is not known zero.
type PingResult = controlpb.PingResult

// TracerouteResult preserves typed hops and optional ReachedTarget (nil differs from false).
type TracerouteResult = controlpb.TracerouteResult

// TracerouteHop is one typed hop observation.
type TracerouteHop = controlpb.TracerouteHop

// PathMTUProbe records one candidate IP MTU test, in bytes.
type PathMTUProbe = controlpb.PathMtuProbe

// PathMTUResult contains discovery evidence; inspect Discovered before using PathMtuBytes.
type PathMTUResult = controlpb.PathMtuResult

// GlobalIPAddress is one discovered public address and its probe outcome.
type GlobalIPAddress = controlpb.GlobalIpAddress

// GlobalIPResult contains per-family public-address observations.
type GlobalIPResult = controlpb.GlobalIpResult

// DownloadResult contains HTTP download measurements; byte counts are not bit counts.
type DownloadResult = controlpb.WgetResult

// DNSResult contains typed DNS answers and acquisition errors.
type DNSResult = controlpb.ResolveDnsResult

// DNSAnswer is one record type/address pair.
type DNSAnswer = controlpb.DnsAnswer

// HTTPResult contains expected/observed status and the agent-side match outcome.
type HTTPResult = controlpb.HttpCheckResult

// NetworkSnapshot contains Android Network observations and their Unix-ms timestamp.
type NetworkSnapshot = controlpb.NetworkSnapshot

// PermissionSnapshot contains acquired permission fields.
type PermissionSnapshot = controlpb.PermissionSnapshot

// WiFiStatusPayload selects CommandResult's Wi-Fi status payload.
type WiFiStatusPayload = controlpb.CommandResult_WifiStatus

// ConnectWiFiPayload selects CommandResult's connect payload.
type ConnectWiFiPayload = controlpb.CommandResult_ConnectWifi

// IPStatusPayload selects CommandResult's IP status payload.
type IPStatusPayload = controlpb.CommandResult_IpStatus

// PingPayload selects CommandResult's ping payload.
type PingPayload = controlpb.CommandResult_Ping

// DNSPayload selects CommandResult's DNS payload.
type DNSPayload = controlpb.CommandResult_ResolveDns

// HTTPPayload selects CommandResult's HTTP payload.
type HTTPPayload = controlpb.CommandResult_HttpCheck

// WiFiDiagnosticsPayload selects CommandResult's diagnostics payload.
type WiFiDiagnosticsPayload = controlpb.CommandResult_WifiDiagnostics

// WiFiScanPayload selects CommandResult's scan payload.
type WiFiScanPayload = controlpb.CommandResult_WifiScan

// WiFiCapabilitiesPayload selects CommandResult's capabilities payload.
type WiFiCapabilitiesPayload = controlpb.CommandResult_WifiCapabilities

// WiFiOperationPayload selects CommandResult's disconnect/reconnect/forget payload.
type WiFiOperationPayload = controlpb.CommandResult_WifiOperation

// WiFiAssertPayload selects CommandResult's wait/assert payload.
type WiFiAssertPayload = controlpb.CommandResult_WifiAssert

// WiFiMonitorPayload selects CommandResult's monitor payload.
type WiFiMonitorPayload = controlpb.CommandResult_WifiMonitor

// WiFiScanDetailPayload selects CommandResult's detailed scan payload.
type WiFiScanDetailPayload = controlpb.CommandResult_WifiScanDetail

// WiFiCyclePayload selects CommandResult's cycle payload.
type WiFiCyclePayload = controlpb.CommandResult_WifiCycle

// TraceroutePayload selects CommandResult's traceroute payload.
type TraceroutePayload = controlpb.CommandResult_Traceroute

// DownloadPayload selects CommandResult's download payload.
type DownloadPayload = controlpb.CommandResult_Wget

// PathMTUPayload selects CommandResult's path-MTU payload.
type PathMTUPayload = controlpb.CommandResult_PathMtu

// GlobalIPPayload selects CommandResult's public-address payload.
type GlobalIPPayload = controlpb.CommandResult_GlobalIp

// NetworkSnapshotPayload selects CommandResult's Android Network snapshot payload.
type NetworkSnapshotPayload = controlpb.CommandResult_NetworkSnapshot

// PermissionSnapshotPayload selects CommandResult's permissions payload.
type PermissionSnapshotPayload = controlpb.CommandResult_PermissionSnapshot
