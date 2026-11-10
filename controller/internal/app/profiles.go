package app

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/acquire"
	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/control"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/pipeline"
	"github.com/mi2428/dropcheck-agent/controller/internal/render"
	"github.com/mi2428/dropcheck-agent/controller/internal/runner"
	"github.com/mi2428/dropcheck-agent/controller/internal/tuirun"
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

func (e linkNetworkExpectation) Evaluate(r tuirun.Result) []tuirun.Finding {
	selected := r.Run.Raw.GetIpStatus()
	if selected == nil || selected.GetNetworkId() == "" || selected.GetInterfaceName() == "" || !slices.Contains(selected.GetTransports(), "wifi") || slices.Contains(selected.GetTransports(), "vpn") {
		return []tuirun.Finding{tuirun.MissingFinding("link.network", "<missing>", e.Description(), "physical Wi-Fi Network/interface unavailable")}
	}
	if known, _ := acquire.Availability(selected.GetObservationFields(), "capabilities"); !known {
		return []tuirun.Finding{tuirun.MissingFinding("link.network", "<missing>", e.Description(), "Network transport unavailable")}
	}
	if known, _ := acquire.Availability(selected.GetObservationFields(), "link_properties"); !known {
		return []tuirun.Finding{tuirun.MissingFinding("link.network", "<missing>", e.Description(), "Network interface unavailable")}
	}
	if known, _ := acquire.Availability(selected.GetWifi().GetObservationFields(), "identity"); !known || selected.GetWifi().GetSsid() == "" || strings.EqualFold(selected.GetWifi().GetSsid(), "<unknown ssid>") {
		return []tuirun.Finding{tuirun.MissingFinding("link.network", "<missing>", e.Description(), "selected IP Network Wi-Fi identity unavailable or mismatched")}
	}
	if observedSSID := selected.GetWifi().GetSsid(); observedSSID != e.ssid {
		finding := tuirun.MissingFinding("wifi.ssid", observedSSID, e.ssid, "selected Wi-Fi SSID differs from requested target")
		finding.ObservedValue, finding.ExpectedValue = observedSSID, e.ssid
		return []tuirun.Finding{finding}
	}
	e.identity.Lock()
	defer e.identity.Unlock()
	observed := selected.GetNetworkId() + "/" + selected.GetInterfaceName()
	if e.identity.network == "" {
		e.identity.network, e.identity.iface = selected.GetNetworkId(), selected.GetInterfaceName()
	}
	expected := e.identity.network + "/" + e.identity.iface
	if e.identity.network == "" || observed != expected {
		finding := tuirun.MissingFinding("link.network", observed, expected, "selected Network changed since initial observation")
		finding.ObservedValue, finding.ExpectedValue = observed, expected
		return []tuirun.Finding{finding}
	}
	finding := tuirun.Pass("link.network", observed, expected)
	finding.ObservedValue, finding.ExpectedValue = observed, expected
	return []tuirun.Finding{finding}
}

// linkPlan observes only the SSID-selected physical Wi-Fi Network. Both
// snapshots detect observed changes, not transient roaming during an operation.
func linkPlan(ssid, family string) tuirun.Plan {
	identity := &linkIdentity{}
	expectations := []tuirun.Expectation{linkNetworkExpectation{identity, ssid}}
	if family == "ipv4" || family == "all" {
		expectations = append(expectations, linkProvisioningExpectation("ipv4"))
	}
	if family == "ipv6" || family == "all" {
		expectations = append(expectations, linkProvisioningExpectation("ipv6"))
	}
	checks := []tuirun.Check{
		tuirun.NewCheck("link "+family+" provisioning", "", command.IPStatusOperation(), tuirun.Policy{}, true, expectations...),
		tuirun.NewCheck("link "+family+" final observation", "", command.IPStatusOperation(), tuirun.Policy{}, true, expectations...),
	}
	return tuirun.Plan{Name: "link", Networks: []tuirun.Network{{Name: "link " + ssid, SSID: ssid, ReadOnly: true}}, Checks: checks}
}

// linkProvisioningExpectation uses typed family-specific observations only.
type linkProvisioningExpectation string

func (e linkProvisioningExpectation) Description() string {
	return string(e) + " address/default route/DNS >=1"
}

func (e linkProvisioningExpectation) Evaluate(result tuirun.Result) []tuirun.Finding {
	family := string(e)
	ip := result.Run.Raw.GetIpStatus()
	known, reason := acquire.Availability(ip.GetObservationFields(), "link_properties")
	counts := [3]int{}
	for _, value := range ip.GetAddresses() {
		addr, err := netip.ParseAddr(value)
		if prefix, prefixErr := netip.ParsePrefix(value); prefixErr == nil {
			addr, err = prefix.Addr(), nil
		}
		if err == nil && addr.Is4() == (family == "ipv4") {
			counts[0]++
		}
	}
	for _, value := range ip.GetRoutes() {
		fields := strings.Fields(value)
		if len(fields) == 0 {
			continue
		}
		prefix, err := netip.ParsePrefix(fields[0])
		if err == nil && prefix.Bits() == 0 && prefix.Addr().IsUnspecified() && prefix.Addr().Is4() == (family == "ipv4") {
			counts[1]++
		}
	}
	for _, value := range ip.GetDnsServers() {
		if addr, err := netip.ParseAddr(value); err == nil && addr.Is4() == (family == "ipv4") {
			counts[2]++
		}
	}
	var findings []tuirun.Finding
	for i, metric := range []string{"address_count", "route_entry_count", "dns_server_count"} {
		metric = "ip." + family + "_" + metric
		if ip == nil || !known {
			findings = append(findings, tuirun.MissingFinding(metric, "<missing>", ">= 1", reason))
		} else if counts[i] < 1 {
			findings = append(findings, tuirun.Fail(metric, "0", ">= 1", "constraint failed"))
		} else {
			findings = append(findings, tuirun.Pass(metric, fmt.Sprint(counts[i]), ">= 1"))
		}
		if ip != nil && known {
			findings[len(findings)-1].ObservedValue, findings[len(findings)-1].ExpectedValue = counts[i], 1
		}
	}
	return findings
}

// labPlan permits traffic on an existing connection but never owns Wi-Fi
// configuration. Its two identity snapshots detect only observed changes.
func labPlan(ssid, family string, config command.LabConfig) (tuirun.Plan, error) {
	base := linkPlan(ssid, family)
	policy := tuirun.Policy{Attempts: 1, Repeat: 1}
	queryType := "A"
	if family == "ipv6" {
		queryType = "AAAA"
	}
	timeout := fmt.Sprint(config.TimeoutMs)
	pingOp, pingErr := command.PingOperation(command.PingOptions{Host: config.Gateway, Family: family, Count: "3", Timeout: timeout})
	dnsOp, dnsErr := command.DNSOperation(config.DNS, queryType, timeout)
	httpOp, httpErr := command.HTTPOperation(config.HTTP, fmt.Sprint(config.ExpectedStatus), timeout)
	for _, err := range []error{pingErr, dnsErr, httpErr} {
		if err != nil {
			return tuirun.Plan{}, err
		}
	}
	pingExpect, pingExpectErr := tuirun.CompileExpectations(map[string]any{"received": ">= 1"})
	dnsExpect, dnsExpectErr := tuirun.CompileExpectations(map[string]any{"answer_count": ">= 1"})
	for _, err := range []error{pingExpectErr, dnsExpectErr} {
		if err != nil {
			return tuirun.Plan{}, err
		}
	}
	checks := []tuirun.Check{base.Checks[0],
		tuirun.NewCheck("ping "+config.Gateway, "", pingOp, policy, true, pingExpect...),
		tuirun.NewCheck("dns "+config.DNS, "", dnsOp, policy, true, dnsExpect...),
		tuirun.NewCheck("http "+config.HTTP, "", httpOp, policy, true, labHTTPStatus(config.ExpectedStatus)),
		base.Checks[1],
	}
	return tuirun.Plan{Name: "lab", Networks: []tuirun.Network{{Name: "lab " + ssid, SSID: ssid, ReadOnly: true, ActiveProbes: true}}, Checks: checks}, nil
}

type labHTTPStatus uint32

func (s labHTTPStatus) Description() string { return fmt.Sprintf("HTTP status=%d and matched=true", s) }

func (s labHTTPStatus) Evaluate(result tuirun.Result) []tuirun.Finding {
	value := result.Run.Raw.GetHttpCheck()
	if value == nil {
		return []tuirun.Finding{tuirun.MissingFinding("http.status", "<missing>", s.Description(), "HTTP result unavailable")}
	}
	if value.GetStatus() != uint32(s) || !value.GetMatched() {
		return []tuirun.Finding{tuirun.Fail("http.status", fmt.Sprint(value.GetStatus()), s.Description(), "expected HTTP response not matched")}
	}
	return []tuirun.Finding{tuirun.Pass("http.status", fmt.Sprint(value.GetStatus()), s.Description())}
}

var profileCatalogue = []struct {
	Name          string `json:"name"`
	Status        string `json:"status"`
	Purpose       string `json:"purpose"`
	Prerequisites string `json:"prerequisites"`
	Family        string `json:"family"`
	Cost          string `json:"cost"`
}{
	{"link", "supported", "existing physical Wi-Fi identity/IP/default route/DNS >=1; validated observation only", "literal SSID and selected Network/interface; BSSID pin unsupported", "ipv4 default, or explicit ipv6/all", "two read-only observations; no external traffic/mutation"},
	{"lab", "configured only", "existing-network gateway ping, selected-resolver DNS, lab HTTP with two identity snapshots", "explicit SSID/family/gateway/dns/resolver selected/http/expected-status/timeout; no URL query", "ipv4 or ipv6; HTTP hostname address family is not pinned", "2 IP reads, 3 ping packets, 1 DNS query, 1 HTTP request; no Wi-Fi mutation"},
	{"internet", "unsupported", "external DNS/HTTP/global-IP", "authorized endpoints and portable contract not configured; no public fallback", "unconfigured", "zero operations"},
	{"eht", "unsupported", "EHT band/MLO/scan", "required expectations and portable contract not configured", "n/a", "zero operations"},
}

func showProfiles() (string, error) {
	var b strings.Builder
	for _, entry := range profileCatalogue {
		fmt.Fprintf(&b, "%s (%s)\n", entry.Name, entry.Status)
		b.WriteString(render.Fields([][2]string{{"Purpose", entry.Purpose}, {"Prerequisites", entry.Prerequisites}, {"Family", entry.Family}, {"Cost", entry.Cost}}, render.Presentation{}))
	}
	b.WriteString("Default: none\n")
	return b.String(), nil
}

var preflightSequence atomic.Uint64

// profileReport records a failed attempt without contacting the handset. Its
// callers supply only fixed, credential-free reason/profile strings.
func profileReport(profile, reason, agentID string) tuirun.Report {
	now := time.Now()
	return tuirun.Report{RunID: fmt.Sprintf("preflight-%d-%d", now.UnixNano(), preflightSequence.Add(1)), Started: now, Ended: now, State: "failed", Outcome: tuirun.MissingOutcome, Counts: tuirun.Counts{Missing: 1}, Steps: []tuirun.StepReport{{Name: profile + " prerequisite", Outcome: tuirun.MissingOutcome, Reason: reason, Scope: tuirun.Scope{Kind: tuirun.ScopeCheck, AgentID: agentID, TargetID: "target/0", CheckID: "prerequisite"}}}}
}

func unsupportedProfileReport(profile, reason, agentID string) tuirun.Report {
	report := profileReport(profile, reason, agentID)
	report.Outcome = tuirun.SkipOutcome
	report.Counts = tuirun.Counts{Skipped: 1}
	report.Steps[0].Outcome = tuirun.SkipOutcome
	report.Steps[0].SkipReason = tuirun.SkipUnsupported
	return report
}

func executeProfile(ctx context.Context, state *shellState, name, ssid, family, bssid string, r acquire.Runner) (tuirun.Report, error) {
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
	if strings.TrimSpace(ssid) == "" || family != "ipv4" && family != "ipv6" && family != "all" {
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
	compiled, err := tuirun.Compile(linkPlan(ssid, family), []control.AgentInfo{agent})
	if err != nil {
		return profileReport("link", "link plan cannot compile", state.selected), nil
	}
	return tuirun.Execute(ctx, compiled, r, tuirun.ExecuteOptions{Rounds: 1})
}

func (s *shellState) runCheck(ctx context.Context, name, ssid, family, bssid, rejection string, pipe pipeline.Pipeline, lab ...command.LabConfig) error {
	var report tuirun.Report
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
	} else if name == "lab" {
		if len(lab) != 1 || !lab[0].ValidFor(family) || ssid == "" || s.targetAll || s.failedUse {
			report = profileReport("lab", "lab configuration or selected target unavailable", s.selected)
		} else if agent, selectedErr := selectedAgent(s); selectedErr != nil {
			report = profileReport("lab", "selected agent unavailable or ambiguous", s.selected)
		} else {
			plan, planErr := labPlan(ssid, family, lab[0])
			if planErr != nil {
				report = profileReport("lab", "lab plan cannot compile", s.selected)
			} else if compiled, compileErr := tuirun.Compile(plan, []control.AgentInfo{agent}); compileErr != nil {
				report = profileReport("lab", "lab plan cannot compile", s.selected)
			} else {
				report, err = tuirun.Execute(ctx, compiled, runner.New(s.server), tuirun.ExecuteOptions{Rounds: 1})
			}
		}
	} else {
		report, err = executeProfile(ctx, s, name, ssid, family, bssid, runner.New(s.server))
	}
	if report.Started.IsZero() {
		report = profileReport("link", "check could not start", s.selected)
	}
	s.lastReport = &report
	s.lastProfile, s.lastSSID = name, ssid
	if name != "link" && name != "lab" {
		s.lastSSID = ""
		if name != "lab" && name != "internet" && name != "eht" {
			s.lastProfile = "unsupported"
		}
	}
	view := terminalPresentation()
	if pipe.StageCount() > 0 || pipe.DisplayJSON() {
		view.Width = 0
	}
	out, renderErr := renderProfileReport(s, false, view)
	if renderErr != nil {
		return renderErr
	}
	out, renderErr = shellOutput(pipe, out, report.Passed() && err == nil)
	if renderErr != nil {
		return renderErr
	}
	fmt.Print(out)
	if !report.Passed() {
		fmt.Fprintf(os.Stderr, "check %s: %s\n", s.lastProfile, report.Outcome)
	}
	return err
}

func renderProfileReport(s *shellState, detail bool, views ...render.Presentation) (string, error) {
	if s.lastReport == nil {
		return "No report in this Shell (historical results never authorize a new check).\n", nil
	}
	view := terminalPresentation()
	if len(views) > 0 {
		view = views[0]
	}
	view.Detail = detail
	out, err := render.Report(*s.lastReport, view)
	if err != nil {
		return "", err
	}
	validated := "?"
	for _, step := range s.lastReport.Steps {
		if !strings.HasPrefix(step.Name, "link ") || len(step.Attempts) == 0 {
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
		if ok, _ := acquire.Availability(ip.GetObservationFields(), "capabilities"); ok {
			validated = fmt.Sprint(ip.GetValidated())
		}
	}
	metadata := render.Fields([][2]string{{"Android validated", validated + " (observation only)"}}, view)
	return fmt.Sprintf("Historical check %s ssid %q at %s (not current readiness)\n%s%s", s.lastProfile, s.lastSSID, s.lastReport.Started.Format(time.RFC3339), metadata, out), nil
}
