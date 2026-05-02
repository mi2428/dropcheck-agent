package festival

import (
	"strconv"
	"time"

	"dropcheck/controller/internal/command"
)

// Check is one executable festival check.
type Check interface {
	Name() string
	build() (step, error)
}

type step struct {
	name         string
	operation    command.Operation
	expectations []Expectation
}

type checkBase struct {
	name         string
	expectations []Expectation
}

func (c checkBase) Name() string {
	return c.name
}

func (c checkBase) withExpectations(expectations []Expectation) checkBase {
	c.expectations = append([]Expectation(nil), expectations...)
	return c
}

// IPStatusCheck configures an IP provisioning check.
type IPStatusCheck struct {
	checkBase
}

// IPStatus starts an IP status check builder.
//
// IPStatus is the primary festival check for layer-3 provisioning: DHCP,
// RA/SLAAC-derived IPv6 state, assigned addresses, routes, DNS servers, MTU,
// and Android network capabilities. Wi-Fi association properties belong in
// WiFiStatus so a plan can distinguish link-layer failures from IP failures.
func IPStatus() IPStatusCheck {
	return IPStatusCheck{checkBase: checkBase{name: "ip status"}}
}

// Expect attaches expectations to the IP status check.
func (c IPStatusCheck) Expect(expectations ...Expectation) IPStatusCheck {
	c.checkBase = c.withExpectations(expectations)
	return c
}

func (c IPStatusCheck) build() (step, error) {
	return step{name: c.name, operation: command.IPStatusOperation(), expectations: c.expectations}, nil
}

// WiFiStatusCheck configures a Wi-Fi link-state check.
type WiFiStatusCheck struct {
	checkBase
}

// WiFiStatus starts a Wi-Fi status check builder.
//
// WiFiStatus is the primary festival check for layer-2 Wi-Fi properties:
// connected SSID/BSSID, RSSI, frequency, derived channel/band, channel width,
// Wi-Fi standard, link speed, and MLO link metadata.
func WiFiStatus() WiFiStatusCheck {
	return WiFiStatusCheck{checkBase: checkBase{name: "wifi status"}}
}

// Expect attaches expectations to the Wi-Fi status check.
func (c WiFiStatusCheck) Expect(expectations ...Expectation) WiFiStatusCheck {
	c.checkBase = c.withExpectations(expectations)
	return c
}

func (c WiFiStatusCheck) build() (step, error) {
	return step{name: c.name, operation: command.WifiStatusOperation(), expectations: c.expectations}, nil
}

// WiFiScanCheck configures a Wi-Fi scan check.
type WiFiScanCheck struct {
	checkBase
	band    string
	fresh   bool
	timeout string
}

// WiFiScan starts a cached Wi-Fi scan check builder.
//
// Use Fresh when the plan should actively wait for an updated Android scan
// result before evaluating AP advertisements.
func WiFiScan() WiFiScanCheck {
	return WiFiScanCheck{checkBase: checkBase{name: "wifi scan"}}
}

// Band restricts scan results to one Wi-Fi band token.
func (c WiFiScanCheck) Band(value string) WiFiScanCheck {
	c.band = value
	return c
}

// Fresh makes the check request a fresh scan instead of cached results.
func (c WiFiScanCheck) Fresh() WiFiScanCheck {
	c.fresh = true
	c.name = "wifi scan fresh"
	return c
}

// Timeout sets the fresh-scan wait timeout.
func (c WiFiScanCheck) Timeout(value time.Duration) WiFiScanCheck {
	c.timeout = durationMS(value)
	return c
}

// Expect attaches expectations to the Wi-Fi scan check.
func (c WiFiScanCheck) Expect(expectations ...Expectation) WiFiScanCheck {
	c.checkBase = c.withExpectations(expectations)
	return c
}

func (c WiFiScanCheck) build() (step, error) {
	if c.fresh {
		op, err := command.WifiFreshScanOperation(c.band, c.timeout)
		return step{name: c.name, operation: op, expectations: c.expectations}, err
	}
	op, err := command.WifiScanOperation(c.band)
	return step{name: c.name, operation: op, expectations: c.expectations}, err
}

// WiFiScanDetailCheck configures a targeted Wi-Fi scan-detail check.
type WiFiScanDetailCheck struct {
	checkBase
	target string
	band   string
}

// WiFiScanDetail starts a scan-detail check for one SSID or BSSID.
func WiFiScanDetail(target string) WiFiScanDetailCheck {
	return WiFiScanDetailCheck{checkBase: checkBase{name: "wifi scan detail " + target}, target: target}
}

// Band restricts scan-detail results to one Wi-Fi band token.
func (c WiFiScanDetailCheck) Band(value string) WiFiScanDetailCheck {
	c.band = value
	return c
}

// Expect attaches expectations to the Wi-Fi scan-detail check.
func (c WiFiScanDetailCheck) Expect(expectations ...Expectation) WiFiScanDetailCheck {
	c.checkBase = c.withExpectations(expectations)
	return c
}

func (c WiFiScanDetailCheck) build() (step, error) {
	op, err := command.WifiScanDetailOperation(c.target, c.band)
	return step{name: c.name, operation: op, expectations: c.expectations}, err
}

// WiFiCapabilitiesCheck configures a device Wi-Fi capabilities check.
type WiFiCapabilitiesCheck struct {
	checkBase
}

// WiFiCapabilities starts a Wi-Fi capabilities check builder.
func WiFiCapabilities() WiFiCapabilitiesCheck {
	return WiFiCapabilitiesCheck{checkBase: checkBase{name: "wifi capabilities"}}
}

// Expect attaches expectations to the Wi-Fi capabilities check.
func (c WiFiCapabilitiesCheck) Expect(expectations ...Expectation) WiFiCapabilitiesCheck {
	c.checkBase = c.withExpectations(expectations)
	return c
}

func (c WiFiCapabilitiesCheck) build() (step, error) {
	return step{name: c.name, operation: command.WifiCapabilitiesOperation(), expectations: c.expectations}, nil
}

// PingCheck configures an ICMP ping check.
type PingCheck struct {
	checkBase
	host    string
	count   string
	size    string
	timeout string
}

// Ping starts a ping check builder.
func Ping(host string) PingCheck {
	return PingCheck{checkBase: checkBase{name: "ping " + host}, host: host}
}

// Count sets the number of ping probes.
func (c PingCheck) Count(value uint32) PingCheck {
	c.count = strconv.FormatUint(uint64(value), 10)
	return c
}

// Size sets the ping payload size in bytes.
func (c PingCheck) Size(value uint32) PingCheck {
	c.size = strconv.FormatUint(uint64(value), 10)
	return c
}

// Timeout sets the ping operation timeout.
func (c PingCheck) Timeout(value time.Duration) PingCheck {
	c.timeout = durationMS(value)
	return c
}

// Expect attaches expectations to the ping check.
func (c PingCheck) Expect(expectations ...Expectation) PingCheck {
	c.checkBase = c.withExpectations(expectations)
	return c
}

func (c PingCheck) build() (step, error) {
	op, err := command.PingOperation(command.PingOptions{Host: c.host, Count: c.count, Size: c.size, Timeout: c.timeout})
	return step{name: c.name, operation: op, expectations: c.expectations}, err
}

// DNSCheck configures a DNS resolution check.
type DNSCheck struct {
	checkBase
	nameValue string
	qtype     string
	timeout   string
}

// DNS starts a DNS check builder.
func DNS(name string) DNSCheck {
	return DNSCheck{checkBase: checkBase{name: "dns " + name}, nameValue: name}
}

// Type sets the DNS record type token.
func (c DNSCheck) Type(value string) DNSCheck {
	c.qtype = value
	return c
}

// A requests A records.
func (c DNSCheck) A() DNSCheck {
	return c.Type("A")
}

// AAAA requests AAAA records.
func (c DNSCheck) AAAA() DNSCheck {
	return c.Type("AAAA")
}

// All requests A and AAAA records.
func (c DNSCheck) All() DNSCheck {
	return c.Type("ALL")
}

// Timeout sets the DNS operation timeout.
func (c DNSCheck) Timeout(value time.Duration) DNSCheck {
	c.timeout = durationMS(value)
	return c
}

// Expect attaches expectations to the DNS check.
func (c DNSCheck) Expect(expectations ...Expectation) DNSCheck {
	c.checkBase = c.withExpectations(expectations)
	return c
}

func (c DNSCheck) build() (step, error) {
	op, err := command.DNSOperation(c.nameValue, c.qtype, c.timeout)
	return step{name: c.name, operation: op, expectations: c.expectations}, err
}

// GlobalIPCheck configures a public IP check.
type GlobalIPCheck struct {
	checkBase
	family  string
	timeout string
}

// GlobalIP starts a public IP check builder.
func GlobalIP() GlobalIPCheck {
	return GlobalIPCheck{checkBase: checkBase{name: "global-ip"}}
}

// IPv4 requests only IPv4.
func (c GlobalIPCheck) IPv4() GlobalIPCheck {
	c.family = "ipv4"
	return c
}

// IPv6 requests only IPv6.
func (c GlobalIPCheck) IPv6() GlobalIPCheck {
	c.family = "ipv6"
	return c
}

// All requests IPv4 and IPv6.
func (c GlobalIPCheck) All() GlobalIPCheck {
	c.family = "all"
	return c
}

// Timeout sets the public IP operation timeout.
func (c GlobalIPCheck) Timeout(value time.Duration) GlobalIPCheck {
	c.timeout = durationMS(value)
	return c
}

// Expect attaches expectations to the public IP check.
func (c GlobalIPCheck) Expect(expectations ...Expectation) GlobalIPCheck {
	c.checkBase = c.withExpectations(expectations)
	return c
}

func (c GlobalIPCheck) build() (step, error) {
	op, err := command.GlobalIPOperation(c.family, c.timeout)
	return step{name: c.name, operation: op, expectations: c.expectations}, err
}

// PathMTUCheck configures a path-MTU discovery check.
type PathMTUCheck struct {
	checkBase
	host    string
	minMTU  string
	maxMTU  string
	timeout string
}

// PathMTU starts a path-MTU check builder.
func PathMTU(host string) PathMTUCheck {
	return PathMTUCheck{checkBase: checkBase{name: "path-mtu " + host}, host: host}
}

// Min sets the minimum MTU search bound.
func (c PathMTUCheck) Min(value uint32) PathMTUCheck {
	c.minMTU = strconv.FormatUint(uint64(value), 10)
	return c
}

// Max sets the maximum MTU search bound.
func (c PathMTUCheck) Max(value uint32) PathMTUCheck {
	c.maxMTU = strconv.FormatUint(uint64(value), 10)
	return c
}

// Timeout sets the path-MTU operation timeout.
func (c PathMTUCheck) Timeout(value time.Duration) PathMTUCheck {
	c.timeout = durationMS(value)
	return c
}

// Expect attaches expectations to the path-MTU check.
func (c PathMTUCheck) Expect(expectations ...Expectation) PathMTUCheck {
	c.checkBase = c.withExpectations(expectations)
	return c
}

func (c PathMTUCheck) build() (step, error) {
	op, err := command.PathMTUOperation(command.PathMTUOptions{Host: c.host, MinMTU: c.minMTU, MaxMTU: c.maxMTU, Timeout: c.timeout})
	return step{name: c.name, operation: op, expectations: c.expectations}, err
}

// TracerouteCheck configures a traceroute check.
type TracerouteCheck struct {
	checkBase
	host    string
	maxHops string
	via     []string
	size    string
	timeout string
}

// Traceroute starts a traceroute check builder.
func Traceroute(host string) TracerouteCheck {
	return TracerouteCheck{checkBase: checkBase{name: "traceroute " + host}, host: host}
}

// MaxHops sets the maximum hop count.
func (c TracerouteCheck) MaxHops(value uint32) TracerouteCheck {
	c.maxHops = strconv.FormatUint(uint64(value), 10)
	return c
}

// Via records a required hop value for command-local rendering options.
func (c TracerouteCheck) Via(value string) TracerouteCheck {
	c.via = append(c.via, value)
	return c
}

// Size sets the traceroute probe payload size.
func (c TracerouteCheck) Size(value uint32) TracerouteCheck {
	c.size = strconv.FormatUint(uint64(value), 10)
	return c
}

// Timeout sets the traceroute operation timeout.
func (c TracerouteCheck) Timeout(value time.Duration) TracerouteCheck {
	c.timeout = durationMS(value)
	return c
}

// Expect attaches expectations to the traceroute check.
func (c TracerouteCheck) Expect(expectations ...Expectation) TracerouteCheck {
	c.checkBase = c.withExpectations(expectations)
	return c
}

func (c TracerouteCheck) build() (step, error) {
	op, err := command.TracerouteOperation(command.TracerouteOptions{Host: c.host, MaxHops: c.maxHops, Via: c.via, Size: c.size, Timeout: c.timeout})
	return step{name: c.name, operation: op, expectations: c.expectations}, err
}

// HTTPCheck configures an HTTP status check.
type HTTPCheck struct {
	checkBase
	url            string
	expectedStatus string
	timeout        string
}

// HTTP starts an HTTP status check builder.
func HTTP(url string) HTTPCheck {
	return HTTPCheck{checkBase: checkBase{name: "http " + url}, url: url}
}

// ExpectedStatus sets the HTTP status expected by the agent.
func (c HTTPCheck) ExpectedStatus(value uint32) HTTPCheck {
	c.expectedStatus = strconv.FormatUint(uint64(value), 10)
	return c
}

// Timeout sets the HTTP operation timeout.
func (c HTTPCheck) Timeout(value time.Duration) HTTPCheck {
	c.timeout = durationMS(value)
	return c
}

// Expect attaches expectations to the HTTP check.
func (c HTTPCheck) Expect(expectations ...Expectation) HTTPCheck {
	c.checkBase = c.withExpectations(expectations)
	return c
}

func (c HTTPCheck) build() (step, error) {
	op, err := command.HTTPOperation(c.url, c.expectedStatus, c.timeout)
	return step{name: c.name, operation: op, expectations: c.expectations}, err
}

// DownloadCheck configures an HTTP download check.
type DownloadCheck struct {
	checkBase
	url     string
	timeout string
}

// Download starts a download check builder.
func Download(url string) DownloadCheck {
	return DownloadCheck{checkBase: checkBase{name: "download " + url}, url: url}
}

// Timeout sets the download operation timeout.
func (c DownloadCheck) Timeout(value time.Duration) DownloadCheck {
	c.timeout = durationMS(value)
	return c
}

// Expect attaches expectations to the download check.
func (c DownloadCheck) Expect(expectations ...Expectation) DownloadCheck {
	c.checkBase = c.withExpectations(expectations)
	return c
}

func (c DownloadCheck) build() (step, error) {
	op, err := command.DownloadOperation(c.url, c.timeout)
	return step{name: c.name, operation: op, expectations: c.expectations}, err
}
