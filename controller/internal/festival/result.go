package festival

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/runner"
	"google.golang.org/protobuf/proto"
)

// ResultSource loads one or more saved measurement targets for offline
// Dropcheck Festival evaluation.
type ResultSource interface {
	// Name returns the subtest name for this source.
	Name() string
	// Targets returns standalone Wi-Fi target results to evaluate.
	Targets() ([]ResultTarget, error)
}

// ResultTarget is one standalone Wi-Fi target result extracted from a source.
type ResultTarget struct {
	// Name is the subtest name for this target.
	Name string
	// SourceName identifies the source that produced this target.
	SourceName string
	// Archive is the full standalone run archive containing the target.
	Archive *controlpb.StandaloneRunArchive
	// WifiGroupIndex is the standalone archive group index.
	WifiGroupIndex uint32
	// WifiGroupName is the standalone archive group name.
	WifiGroupName string
	// Steps are the archived steps for this Wi-Fi target.
	Steps []*controlpb.StandaloneMeasurementStep
}

// StandaloneArchive evaluates one in-memory standalone run archive.
func StandaloneArchive(name string, archive *controlpb.StandaloneRunArchive) ResultSource {
	return standaloneArchiveSource{name: name, archive: cloneStandaloneArchive(archive), archiveSet: true}
}

// StandaloneArchiveBytes evaluates a protobuf-encoded StandaloneRunArchive.
func StandaloneArchiveBytes(name string, data []byte) ResultSource {
	return standaloneArchiveSource{name: name, data: append([]byte(nil), data...)}
}

// StandaloneArchiveFile evaluates a protobuf-encoded StandaloneRunArchive file.
func StandaloneArchiveFile(path string) ResultSource {
	return standaloneArchiveSource{name: filepath.Base(path), path: path}
}

type standaloneArchiveSource struct {
	name    string
	path    string
	data    []byte
	archive *controlpb.StandaloneRunArchive
	// archiveSet preserves the constructor choice so nil in-memory archives
	// report as nil archives instead of empty byte buffers.
	archiveSet bool
}

func (s standaloneArchiveSource) Name() string {
	switch {
	case strings.TrimSpace(s.name) != "":
		return strings.TrimSpace(s.name)
	case s.path != "":
		return filepath.Base(s.path)
	default:
		return "standalone_archive"
	}
}

func (s standaloneArchiveSource) Targets() ([]ResultTarget, error) {
	archive, err := s.loadArchive()
	if err != nil {
		return nil, err
	}
	return standaloneArchiveTargets(s.Name(), archive)
}

func (s standaloneArchiveSource) loadArchive() (*controlpb.StandaloneRunArchive, error) {
	switch {
	case s.archiveSet:
		return cloneStandaloneArchive(s.archive), nil
	case s.path != "":
		data, err := os.ReadFile(s.path)
		if err != nil {
			return nil, err
		}
		return unmarshalStandaloneArchive(data)
	default:
		return unmarshalStandaloneArchive(s.data)
	}
}

func unmarshalStandaloneArchive(data []byte) (*controlpb.StandaloneRunArchive, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("standalone archive data is empty")
	}
	archive := &controlpb.StandaloneRunArchive{}
	if err := proto.Unmarshal(data, archive); err != nil {
		return nil, fmt.Errorf("decode standalone archive: %w", err)
	}
	return archive, nil
}

func cloneStandaloneArchive(archive *controlpb.StandaloneRunArchive) *controlpb.StandaloneRunArchive {
	if archive == nil {
		return nil
	}
	return proto.Clone(archive).(*controlpb.StandaloneRunArchive)
}

func standaloneArchiveTargets(sourceName string, archive *controlpb.StandaloneRunArchive) ([]ResultTarget, error) {
	if archive == nil {
		return nil, fmt.Errorf("standalone archive is nil")
	}
	steps := archive.GetSteps()
	if len(steps) == 0 {
		return nil, fmt.Errorf("standalone archive has no measurement steps")
	}
	indexByKey := map[string]int{}
	var targets []ResultTarget
	for _, step := range steps {
		if step == nil {
			continue
		}
		key := standaloneGroupKey(step)
		targetIndex, ok := indexByKey[key]
		if !ok {
			target := ResultTarget{
				Name:           standaloneGroupDisplayName(archive, step.GetWifiGroupIndex(), step.GetWifiGroupName()),
				SourceName:     sourceName,
				Archive:        archive,
				WifiGroupIndex: step.GetWifiGroupIndex(),
				WifiGroupName:  step.GetWifiGroupName(),
			}
			targets = append(targets, target)
			targetIndex = len(targets) - 1
			indexByKey[key] = targetIndex
		}
		targets[targetIndex].Steps = append(targets[targetIndex].Steps, step)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("standalone archive has no usable measurement steps")
	}
	return targets, nil
}

func standaloneGroupKey(step *controlpb.StandaloneMeasurementStep) string {
	if step.GetWifiGroupIndex() > 0 {
		return fmt.Sprintf("index:%d", step.GetWifiGroupIndex())
	}
	if step.GetWifiGroupName() != "" {
		return "name:" + step.GetWifiGroupName()
	}
	return "default"
}

func standaloneGroupDisplayName(archive *controlpb.StandaloneRunArchive, index uint32, name string) string {
	switch {
	case strings.TrimSpace(name) != "":
		return strings.TrimSpace(name)
	case index > 0:
		return fmt.Sprintf("wifi_group_%d", index)
	case archive.GetSummary().GetRunId() != "":
		return archive.GetSummary().GetRunId()
	case archive.GetFesta().GetName() != "":
		return archive.GetFesta().GetName()
	default:
		return "standalone_result"
	}
}

func resultSourceName(source ResultSource) string {
	if source == nil {
		return "nil_result_source"
	}
	if name := strings.TrimSpace(source.Name()); name != "" {
		return name
	}
	return "standalone_results"
}

func (target ResultTarget) displayName() string {
	if name := strings.TrimSpace(target.Name); name != "" {
		return name
	}
	return standaloneGroupDisplayName(target.Archive, target.WifiGroupIndex, target.WifiGroupName)
}

func (target ResultTarget) stepNamed(name string) *controlpb.StandaloneMeasurementStep {
	for _, step := range target.Steps {
		if step.GetStepName() == name {
			return step
		}
	}
	return nil
}

func (target ResultTarget) syntheticNetwork() Network {
	network := Network{name: target.displayName()}
	for _, step := range target.Steps {
		command := step.GetCommand()
		if connect := command.GetConnectWifi(); connect != nil {
			network.ssid = connect.GetSsid()
			network.bssid = connect.GetBssid()
			return network
		}
		if wait := command.GetWaitWifiConnected(); wait != nil {
			network.ssid = wait.GetSsid()
			network.bssid = wait.GetBssid()
			return network
		}
	}
	return network
}

func (target ResultTarget) syntheticAgent() control.AgentInfo {
	return control.AgentInfo{ID: "standalone:" + target.displayName()}
}

func (target ResultTarget) matchCommand(command *controlpb.RunCommand) *controlpb.StandaloneMeasurementStep {
	for _, step := range target.Steps {
		if runCommandsMatch(command, step.GetCommand()) {
			return step
		}
	}
	return nil
}

type archiveRunner struct {
	target ResultTarget
}

func (r archiveRunner) Run(_ context.Context, _ control.AgentInfo, op command.Operation) (runner.Result, error) {
	cmd, options, err := command.BuildRunCommand(op)
	result := runner.Result{Operation: op, Command: cmd, Options: options}
	if err != nil {
		return result, err
	}
	step := r.target.matchCommand(cmd)
	if step == nil {
		return result, fmt.Errorf("standalone result %q has no archived %s step matching %s", r.target.displayName(), op.Name, commandLabel(cmd))
	}
	result.CommandID = archiveCommandID(r.target, step)
	result.Result = archivedStepResult(step)
	return result, nil
}

func archiveCommandID(target ResultTarget, step *controlpb.StandaloneMeasurementStep) string {
	parts := []string{"standalone"}
	if target.SourceName != "" {
		parts = append(parts, target.SourceName)
	}
	parts = append(parts, target.displayName(), fmt.Sprintf("%d", step.GetStepIndex()))
	return strings.Join(parts, ":")
}

func archivedStepResult(step *controlpb.StandaloneMeasurementStep) *controlpb.CommandResult {
	if step.GetError() != "" {
		return failedCommandResult(step.GetError())
	}
	if step.GetResult() == nil {
		return failedCommandResult("archived step has no command result")
	}
	return proto.Clone(step.GetResult()).(*controlpb.CommandResult)
}

func failedCommandResult(message string) *controlpb.CommandResult {
	return &controlpb.CommandResult{
		Status:  controlpb.CommandResult_STATUS_FAILED,
		Message: message,
	}
}

func commandLabel(cmd *controlpb.RunCommand) string {
	if cmd == nil {
		return "<nil>"
	}
	if cmd.GetLabel() != "" {
		return cmd.GetLabel()
	}
	return fmt.Sprintf("%T", cmd.GetCommand())
}

func runCommandsMatch(want *controlpb.RunCommand, got *controlpb.RunCommand) bool {
	if want == nil || got == nil {
		return false
	}
	switch wantCommand := want.GetCommand().(type) {
	case *controlpb.RunCommand_GetWifiStatus:
		return got.GetGetWifiStatus() != nil
	case *controlpb.RunCommand_GetIpStatus:
		return got.GetGetIpStatus() != nil
	case *controlpb.RunCommand_GetWifiCapabilities:
		return got.GetGetWifiCapabilities() != nil
	case *controlpb.RunCommand_GetWifiScan:
		return sameWifiScan(wantCommand.GetWifiScan, got.GetGetWifiScan())
	case *controlpb.RunCommand_GetFreshWifiScan:
		return sameFreshWifiScan(wantCommand.GetFreshWifiScan, got.GetGetFreshWifiScan())
	case *controlpb.RunCommand_GetWifiScanDetail:
		return sameWifiScanDetail(wantCommand.GetWifiScanDetail, got.GetGetWifiScanDetail())
	case *controlpb.RunCommand_ConnectWifi:
		return sameConnectWifi(wantCommand.ConnectWifi, got.GetConnectWifi())
	case *controlpb.RunCommand_WaitWifiConnected:
		return sameWaitWifiConnected(wantCommand.WaitWifiConnected, got.GetWaitWifiConnected())
	case *controlpb.RunCommand_AssertWifi:
		return sameAssertWifi(wantCommand.AssertWifi, got.GetAssertWifi())
	case *controlpb.RunCommand_Ping:
		return samePing(wantCommand.Ping, got.GetPing())
	case *controlpb.RunCommand_ResolveDns:
		return sameResolveDNS(wantCommand.ResolveDns, got.GetResolveDns())
	case *controlpb.RunCommand_HttpCheck:
		return sameHTTPCheck(wantCommand.HttpCheck, got.GetHttpCheck())
	case *controlpb.RunCommand_GlobalIp:
		return sameGlobalIP(wantCommand.GlobalIp, got.GetGlobalIp())
	case *controlpb.RunCommand_PathMtu:
		return samePathMTU(wantCommand.PathMtu, got.GetPathMtu())
	case *controlpb.RunCommand_Traceroute:
		return sameTraceroute(wantCommand.Traceroute, got.GetTraceroute())
	case *controlpb.RunCommand_Wget:
		return sameWget(wantCommand.Wget, got.GetWget())
	default:
		return proto.Equal(want, got)
	}
}

func sameWifiScan(want *controlpb.GetWifiScan, got *controlpb.GetWifiScan) bool {
	return got != nil && want.GetBand() == got.GetBand()
}

func sameFreshWifiScan(want *controlpb.GetFreshWifiScan, got *controlpb.GetFreshWifiScan) bool {
	return got != nil && want.GetBand() == got.GetBand()
}

func sameWifiScanDetail(want *controlpb.GetWifiScanDetail, got *controlpb.GetWifiScanDetail) bool {
	return got != nil && want.GetTarget() == got.GetTarget() && want.GetBand() == got.GetBand()
}

func sameConnectWifi(want *controlpb.ConnectWifi, got *controlpb.ConnectWifi) bool {
	return got != nil &&
		want.GetSsid() == got.GetSsid() &&
		want.GetBssid() == got.GetBssid() &&
		want.GetSecurity() == got.GetSecurity() &&
		want.GetBand() == got.GetBand() &&
		want.GetMacRandomization() == got.GetMacRandomization()
}

func sameWaitWifiConnected(want *controlpb.WaitWifiConnected, got *controlpb.WaitWifiConnected) bool {
	return got != nil &&
		want.GetSsid() == got.GetSsid() &&
		want.GetBssid() == got.GetBssid() &&
		want.GetSecurity() == got.GetSecurity() &&
		want.GetBand() == got.GetBand() &&
		want.GetRequireIp() == got.GetRequireIp() &&
		want.GetRequireValidated() == got.GetRequireValidated()
}

func sameAssertWifi(want *controlpb.AssertWifi, got *controlpb.AssertWifi) bool {
	return got != nil &&
		want.GetSsid() == got.GetSsid() &&
		want.GetBssid() == got.GetBssid() &&
		want.GetSecurity() == got.GetSecurity() &&
		want.GetBand() == got.GetBand() &&
		want.GetRequireIp() == got.GetRequireIp() &&
		want.GetRequireValidated() == got.GetRequireValidated()
}

func samePing(want *controlpb.Ping, got *controlpb.Ping) bool {
	return got != nil &&
		want.GetHost() == got.GetHost() &&
		want.GetCount() == got.GetCount() &&
		want.GetSizeBytes() == got.GetSizeBytes()
}

func sameResolveDNS(want *controlpb.ResolveDns, got *controlpb.ResolveDns) bool {
	return got != nil &&
		want.GetName() == got.GetName() &&
		sameQTypes(want.GetQtypes(), got.GetQtypes())
}

func sameHTTPCheck(want *controlpb.HttpCheck, got *controlpb.HttpCheck) bool {
	return got != nil &&
		normalizeHTTPMatchURL(want.GetUrl()) == normalizeHTTPMatchURL(got.GetUrl()) &&
		want.GetExpectedStatus() == got.GetExpectedStatus()
}

func sameGlobalIP(want *controlpb.GlobalIp, got *controlpb.GlobalIp) bool {
	return got != nil && want.GetFamily() == got.GetFamily()
}

func samePathMTU(want *controlpb.PathMtu, got *controlpb.PathMtu) bool {
	return got != nil &&
		want.GetHost() == got.GetHost() &&
		want.GetMinMtuBytes() == got.GetMinMtuBytes() &&
		want.GetMaxMtuBytes() == got.GetMaxMtuBytes()
}

func sameTraceroute(want *controlpb.Traceroute, got *controlpb.Traceroute) bool {
	return got != nil &&
		want.GetHost() == got.GetHost() &&
		want.GetMaxHops() == got.GetMaxHops() &&
		want.GetSizeBytes() == got.GetSizeBytes()
}

func sameWget(want *controlpb.Wget, got *controlpb.Wget) bool {
	return got != nil && want.GetUrl() == got.GetUrl()
}

func sameQTypes(want []controlpb.DnsRecordType, got []controlpb.DnsRecordType) bool {
	if len(want) != len(got) {
		return false
	}
	counts := make(map[controlpb.DnsRecordType]int, len(want))
	for _, qtype := range want {
		counts[qtype]++
	}
	for _, qtype := range got {
		counts[qtype]--
		if counts[qtype] < 0 {
			return false
		}
	}
	return true
}

func normalizeHTTPMatchURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.Contains(value, "://") {
		return value
	}
	return "https://" + value
}
