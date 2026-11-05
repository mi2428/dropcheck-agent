package dropcheck

import (
	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
)

// These are the same validated operation values used by Shell and watch.
type Operation = command.Operation
type WiFiConnectOptions = command.WifiConnectOptions
type WiFiCycleOptions = command.WifiCycleOptions
type WiFiExpectationOptions = command.WifiExpectationOptions
type WiFiEHTOptions = command.WifiEHTOptions
type PingOptions = command.PingOptions
type TracerouteOptions = command.TracerouteOptions
type PathMTUOptions = command.PathMTUOptions
type NetworkSelector = controlpb.NetworkSelector
type CommandResult = controlpb.CommandResult
type Status = controlpb.CommandResult_Status

const (
	StatusUnspecified = controlpb.CommandResult_STATUS_UNSPECIFIED
	StatusOK          = controlpb.CommandResult_STATUS_OK
	StatusFailed      = controlpb.CommandResult_STATUS_FAILED
	StatusCanceled    = controlpb.CommandResult_STATUS_CANCELED
)

func WiFiStatus() Operation       { return command.WifiStatusOperation() }
func WiFiDiagnostics() Operation  { return command.WifiDiagnosticsOperation() }
func WiFiCapabilities() Operation { return command.WifiCapabilitiesOperation() }
func WiFiDisconnect() Operation   { return command.WifiDisconnectOperation() }
func IPStatus() Operation         { return command.IPStatusOperation() }
func Networks() Operation         { return command.NetworksOperation() }
func Permissions() Operation      { return command.PermissionsOperation() }

func WiFiScan(band string) (Operation, error) { return command.WifiScanOperation(band) }
func WiFiFreshScan(band, timeout string) (Operation, error) {
	return command.WifiFreshScanOperation(band, timeout)
}
func WiFiScanDetail(target, band string) (Operation, error) {
	return command.WifiScanDetailOperation(target, band)
}
func WiFiEHT(opts WiFiEHTOptions) (Operation, error) {
	return command.WifiEHTOperationWithOptions(opts)
}
func WiFiConnect(opts WiFiConnectOptions) (Operation, error) {
	return command.WifiConnectOperation(opts)
}
func WiFiCycle(opts WiFiCycleOptions) (Operation, error) {
	return command.WifiCycleOperation(opts)
}
func WiFiWaitConnected(ssid string, opts WiFiExpectationOptions) (Operation, error) {
	return command.WifiWaitConnectedOperation(ssid, opts)
}
func WiFiAssert(opts WiFiExpectationOptions) (Operation, error) {
	return command.WifiAssertOperation(opts)
}
func WiFiMonitor(duration, interval string) (Operation, error) {
	return command.WifiMonitorOperation(duration, interval)
}
func WiFiReconnect(timeout string) (Operation, error) {
	return command.WifiReconnectOperation(timeout)
}

// WiFiForgetSSID never interprets a numeric SSID as a configuration ID.
func WiFiForgetSSID(ssid string) Operation {
	op := command.WifiForgetOperation("")
	op.Command.GetForgetWifi().Selector = &controlpb.ForgetWifi_Ssid{Ssid: ssid}
	return op
}
func WiFiForgetConfigID(id int32) Operation {
	op := command.WifiForgetOperation("")
	op.Command.GetForgetWifi().Selector = &controlpb.ForgetWifi_ConfigId{ConfigId: id}
	return op
}

func Network(target string) (Operation, error) { return command.NetworkOperation(target) }
func Ping(opts PingOptions) (Operation, error) { return command.PingOperation(opts) }
func Traceroute(opts TracerouteOptions) (Operation, error) {
	return command.TracerouteOperation(opts)
}
func PathMTU(opts PathMTUOptions) (Operation, error) { return command.PathMTUOperation(opts) }
func GlobalIP(family, timeout string) (Operation, error) {
	return command.GlobalIPOperation(family, timeout)
}
func DNS(name, record, timeout string) (Operation, error) {
	return command.DNSOperation(name, record, timeout)
}
func HTTP(url, expectedStatus, timeout string) (Operation, error) {
	return command.HTTPOperation(url, expectedStatus, timeout)
}
func Download(url, timeout string) (Operation, error) {
	return command.DownloadOperation(url, timeout)
}
