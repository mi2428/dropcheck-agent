package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/harness"
	"dropcheck/controller/internal/harness/ip"
	"dropcheck/controller/internal/render"
	"dropcheck/controller/internal/runner"
)

type linkIdentity struct {
	sync.Mutex
	network, iface string
}

type linkNetworkExpectation struct {
	identity *linkIdentity
	ssid     string
}

func (e linkNetworkExpectation) Description() string {
	return "same selected physical Wi-Fi Network/interface and SSID"
}

func (e linkNetworkExpectation) Evaluate(r harness.Result) []harness.Finding {
	selected := r.Run.Raw.GetIpStatus()
	if selected == nil || selected.GetNetworkId() == "" || selected.GetInterfaceName() == "" || !slices.Contains(selected.GetTransports(), "wifi") || slices.Contains(selected.GetTransports(), "vpn") {
		return []harness.Finding{harness.MissingFinding("link.network", "<missing>", e.Description(), "physical Wi-Fi Network/interface unavailable")}
	}
	if known, _ := harness.Availability(selected.GetObservationFields(), "capabilities"); !known {
		return []harness.Finding{harness.MissingFinding("link.network", "<missing>", e.Description(), "Network transport unavailable")}
	}
	if known, _ := harness.Availability(selected.GetObservationFields(), "link_properties"); !known {
		return []harness.Finding{harness.MissingFinding("link.network", "<missing>", e.Description(), "Network interface unavailable")}
	}
	if known, _ := harness.Availability(selected.GetWifi().GetObservationFields(), "identity"); !known || selected.GetWifi().GetSsid() == "" || strings.EqualFold(selected.GetWifi().GetSsid(), "<unknown ssid>") {
		return []harness.Finding{harness.MissingFinding("link.network", "<missing>", e.Description(), "selected IP Network Wi-Fi identity unavailable or mismatched")}
	}
	if observedSSID := selected.GetWifi().GetSsid(); observedSSID != e.ssid {
		finding := harness.MissingFinding("wifi.ssid", observedSSID, e.ssid, "selected Wi-Fi SSID differs from requested target")
		finding.ObservedValue, finding.ExpectedValue = observedSSID, e.ssid
		return []harness.Finding{finding}
	}
	e.identity.Lock()
	defer e.identity.Unlock()
	observed := selected.GetNetworkId() + "/" + selected.GetInterfaceName()
	if e.identity.network == "" {
		e.identity.network, e.identity.iface = selected.GetNetworkId(), selected.GetInterfaceName()
	}
	expected := e.identity.network + "/" + e.identity.iface
	if e.identity.network == "" || observed != expected {
		finding := harness.MissingFinding("link.network", observed, expected, "selected Network changed since initial observation")
		finding.ObservedValue, finding.ExpectedValue = observed, expected
		return []harness.Finding{finding}
	}
	finding := harness.Pass("link.network", observed, expected)
	finding.ObservedValue, finding.ExpectedValue = observed, expected
	return []harness.Finding{finding}
}

// linkPlan observes only the SSID-selected physical Wi-Fi Network. Both
// snapshots detect observed changes, not transient roaming during an operation.
func linkPlan(ssid, family string) harness.Plan {
	identity := &linkIdentity{}
	var address, route, dns harness.Expectation
	if family == "ipv6" {
		address, route, dns = ip.IPv6AddressCount().Ge(1), ip.IPv6ParsedRoute().Default().Count().Ge(1), ip.IPv6DNSServerCount().Ge(1)
	} else {
		address, route, dns = ip.IPv4AddressCount().Ge(1), ip.IPv4ParsedRoute().Default().Count().Ge(1), ip.IPv4DNSServerCount().Ge(1)
	}
	checks := []harness.Check{
		harness.NewCheck("link "+family+" provisioning", "", command.IPStatusOperation(), harness.Policy{}, true, linkNetworkExpectation{identity, ssid}, address, route, dns),
		harness.NewCheck("link "+family+" final observation", "", command.IPStatusOperation(), harness.Policy{}, true, linkNetworkExpectation{identity, ssid}, address, route, dns),
	}
	return harness.Plan{Name: "link", Networks: []harness.Network{harness.ObservedWiFi("link " + ssid).SSID(ssid)}, Checks: checks}
}

var profileCatalogue = []struct {
	Name          string `json:"name"`
	Status        string `json:"status"`
	Purpose       string `json:"purpose"`
	Prerequisites string `json:"prerequisites"`
	Family        string `json:"family"`
	Cost          string `json:"cost"`
}{
	{"link", "supported", "existing physical Wi-Fi identity/IP/default route/DNS >=1; validated observation only", "literal SSID and selected Network/interface; BSSID pin unsupported", "ipv4 default, or explicit ipv6", "two read-only observations; no external traffic/mutation"},
	{"lab", "unsupported", "gateway, lab DNS and lab HTTP", "endpoint/resolver/expected status/portable contract not configured", "unconfigured", "zero operations"},
	{"internet", "unsupported", "external DNS/HTTP/global-IP", "authorized endpoints and portable contract not configured; no public fallback", "unconfigured", "zero operations"},
	{"eht", "unsupported", "EHT band/MLO/scan", "required expectations and portable contract not configured", "n/a", "zero operations"},
}

func profileCandidates() string {
	var names []string
	for _, entry := range profileCatalogue {
		names = append(names, entry.Name)
	}
	return "check requires an exact name (" + strings.Join(names, ", ") + "); no default configured"
}

func showProfiles(format outputFormat) (string, error) {
	if format == outputJSON {
		data, err := json.Marshal(struct {
			Default string `json:"default"`
			Checks  any    `json:"checks"`
		}{Checks: profileCatalogue})
		return string(data) + "\n", err
	}
	var b strings.Builder
	for _, entry := range profileCatalogue {
		fmt.Fprintf(&b, "%s (%s): %s; prerequisites: %s; family: %s; cost: %s\n", entry.Name, entry.Status, entry.Purpose, entry.Prerequisites, entry.Family, entry.Cost)
	}
	b.WriteString("Default: none\n")
	return b.String(), nil
}

var preflightSequence atomic.Uint64

// profileReport records a failed attempt without contacting the handset. Its
// callers supply only fixed, credential-free reason/profile strings.
func profileReport(profile, reason, agentID string) harness.Report {
	now := time.Now()
	return harness.Report{RunID: fmt.Sprintf("preflight-%d-%d", now.UnixNano(), preflightSequence.Add(1)), Started: now, Ended: now, State: "failed", Outcome: harness.MissingOutcome, Counts: harness.Counts{Missing: 1}, Steps: []harness.StepReport{{Name: profile + " prerequisite", Outcome: harness.MissingOutcome, Reason: reason, Scope: harness.Scope{Kind: harness.ScopeCheck, AgentID: agentID, TargetID: "target/0", CheckID: "prerequisite"}}}}
}

func unsupportedProfileReport(profile, reason, agentID string) harness.Report {
	report := profileReport(profile, reason, agentID)
	report.Outcome = harness.SkipOutcome
	report.Counts = harness.Counts{Skipped: 1}
	report.Steps[0].Outcome = harness.SkipOutcome
	report.Steps[0].SkipReason = harness.SkipUnsupported
	return report
}

func executeProfile(ctx context.Context, state *shellState, name, ssid, family, bssid string, r harness.OperationRunner) (harness.Report, error) {
	if name != "link" {
		profile := "unsupported"
		if name == "lab" || name == "internet" || name == "eht" {
			profile = name
		}
		return unsupportedProfileReport(profile, "profile unsupported; candidates: link, lab, internet, eht", state.selected), nil
	}
	if bssid != "" {
		return unsupportedProfileReport("link", "strict BSSID pinning is unsupported by the SSID-only NetworkSelector", state.selected), nil
	}
	if strings.TrimSpace(ssid) == "" || family != "ipv4" && family != "ipv6" {
		return profileReport("link", "invalid link target/family", state.selected), nil
	}
	if state.targetAll {
		return profileReport("link", "check requires exactly one pinned agent", state.selected), nil
	}
	if state.failedUse {
		return profileReport("link", "previous connection attempt failed; target not ready", state.selected), nil
	}
	agent, err := selectedAgent(state)
	if err != nil {
		return profileReport("link", "selected agent unavailable or ambiguous", state.selected), nil
	}
	compiled, err := harness.Compile(linkPlan(ssid, family), []control.AgentInfo{agent})
	if err != nil {
		return profileReport("link", "link plan cannot compile", state.selected), nil
	}
	return harness.Execute(ctx, compiled, r, harness.ExecuteOptions{Rounds: 1})
}

func (s *shellState) runCheck(ctx context.Context, name, ssid, family, bssid, rejection string, format outputFormat, pipe pipePipeline, strict bool) error {
	if name == "" {
		fmt.Fprintln(os.Stdout, profileCandidates())
		return nil
	}
	var report harness.Report
	var err error
	if rejection != "" {
		profile := "link"
		if name == "lab" || name == "internet" || name == "eht" {
			profile = name
		}
		if name != "link" && profile == "link" {
			profile = "unsupported"
		}
		// Malformed input may have put a credential in an SSID-valued token.
		ssid = ""
		if name != "link" || strings.Contains(rejection, "pinning unsupported") || strings.Contains(rejection, "pinning is unsupported") {
			report = unsupportedProfileReport(profile, rejection, s.selected)
		} else {
			report = profileReport(profile, rejection, s.selected)
		}
	} else {
		report, err = executeProfile(ctx, s, name, ssid, family, bssid, runner.New(s.server))
	}
	if report.Started.IsZero() {
		report = profileReport("link", "check could not start", s.selected)
	}
	s.lastReport = &report
	s.lastProfile, s.lastSSID = name, ssid
	if name != "link" {
		s.lastSSID = ""
		if name != "lab" && name != "internet" && name != "eht" {
			s.lastProfile = "unsupported"
		}
	}
	out, renderErr := renderProfileReport(s, format, false)
	if renderErr != nil {
		return renderErr
	}
	out, renderErr = pipe.apply(out)
	if renderErr != nil {
		return renderErr
	}
	fmt.Print(out)
	if strict && (!report.Passed() || err != nil) {
		return fmt.Errorf("check %s: %s", s.lastProfile, report.Outcome)
	}
	return err
}

func renderProfileReport(s *shellState, format outputFormat, detail bool) (string, error) {
	if s.lastReport == nil {
		if format == outputJSON {
			return "{\"report\":null,\"message\":\"no report in this Shell\"}\n", nil
		}
		return "No report in this Shell (historical results never authorize a new check).\n", nil
	}
	if format == outputJSON {
		data, err := json.Marshal(struct {
			Profile    string          `json:"profile"`
			SSID       string          `json:"ssid"`
			Historical bool            `json:"historical"`
			Report     *harness.Report `json:"report"`
		}{s.lastProfile, s.lastSSID, true, s.lastReport})
		return string(data) + "\n", err
	}
	view := terminalPresentation()
	view.Detail = detail
	out, err := render.Report(*s.lastReport, view)
	if err != nil {
		return "", err
	}
	validated := "?"
	for _, step := range s.lastReport.Steps {
		if !strings.HasPrefix(step.Name, "link ipv") || len(step.Attempts) == 0 {
			continue
		}
		result := step.Attempts[len(step.Attempts)-1].Result.Raw
		if result == nil || result.GetStatus() != controlpb.CommandResult_STATUS_OK {
			continue
		}
		ip := result.GetIpStatus()
		if ip == nil {
			continue
		}
		if ok, _ := harness.Availability(ip.GetObservationFields(), "capabilities"); ok {
			validated = fmt.Sprint(ip.GetValidated())
		}
	}
	return fmt.Sprintf("Historical check %s ssid %q at %s (not current readiness)\nAndroid validated: %s (observation only)\n%s", s.lastProfile, s.lastSSID, s.lastReport.Started.Format(time.RFC3339), validated, out), nil
}
