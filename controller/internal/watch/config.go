package watch

import (
	"bytes"
	"dropcheck/controller/internal/harness"
	"fmt"
	"io"
	"os"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Config is the YAML surface consumed by `dropcheck watch -c`.
type Config struct {
	Version       int               `yaml:"version"`
	Name          string            `yaml:"name"`
	RoundInterval Duration          `yaml:"round_interval"`
	Vars          map[string]string `yaml:"vars"`
	Defaults      TargetDefaults    `yaml:"defaults"`
	Targets       []Target          `yaml:"targets"`
	Checks        []Check           `yaml:"checks"`
}

// TargetDefaults contains YAML defaults applied to every configured target.
type TargetDefaults struct {
	Agent            string   `yaml:"agent"`
	Passphrase       string   `yaml:"passphrase"`
	PassphraseEnv    string   `yaml:"passphrase_env"`
	Security         string   `yaml:"security"`
	MacRandomization string   `yaml:"mac_randomization"`
	MacRotation      string   `yaml:"mac_rotation"`
	ConnectTimeout   Duration `yaml:"connect_timeout"`
	WaitTimeout      Duration `yaml:"wait_timeout"`
	RequireIP        *bool    `yaml:"require_ip"`
	RequireValidated *bool    `yaml:"require_validated"`
	DisconnectAfter  *bool    `yaml:"disconnect_after"`
	ForgetAfter      *bool    `yaml:"forget_after"`
}

// Target describes one Wi-Fi association target in a watch plan.
type Target struct {
	Name             string            `yaml:"name"`
	ShortName        string            `yaml:"short_name"`
	Agent            string            `yaml:"agent"`
	SSID             string            `yaml:"ssid"`
	BSSID            string            `yaml:"bssid"`
	Band             string            `yaml:"band"`
	Vars             map[string]string `yaml:"vars"`
	Passphrase       string            `yaml:"passphrase"`
	PassphraseEnv    string            `yaml:"passphrase_env"`
	Security         string            `yaml:"security"`
	MacRandomization string            `yaml:"mac_randomization"`
	MacRotation      string            `yaml:"mac_rotation"`
	ConnectTimeout   Duration          `yaml:"connect_timeout"`
	WaitTimeout      Duration          `yaml:"wait_timeout"`
	RequireIP        *bool             `yaml:"require_ip"`
	RequireValidated *bool             `yaml:"require_validated"`
	DisconnectAfter  *bool             `yaml:"disconnect_after"`
	ForgetAfter      *bool             `yaml:"forget_after"`
	WaitConnected    *bool             `yaml:"wait_connected"`
	Checks           []Check           `yaml:"checks"`
}

// Check describes one probe to run after a target is connected and ready.
type Check struct {
	Name       string         `yaml:"name"`
	Type       string         `yaml:"type"`
	Host       string         `yaml:"host"`
	Count      *uint32        `yaml:"count"`
	SizeBytes  *uint32        `yaml:"size_bytes"`
	MaxHops    *uint32        `yaml:"max_hops"`
	MinMTU     *uint32        `yaml:"min_mtu"`
	MaxMTU     *uint32        `yaml:"max_mtu"`
	Query      string         `yaml:"query"`
	Record     string         `yaml:"record"`
	URL        string         `yaml:"url"`
	Status     *uint32        `yaml:"status"`
	Family     string         `yaml:"family"`
	ScanTarget string         `yaml:"scan_target"`
	Band       string         `yaml:"band"`
	Timeout    Duration       `yaml:"timeout"`
	Required   bool           `yaml:"required"`
	Expect     map[string]any `yaml:"expect"`
	Fresh      bool           `yaml:"fresh"`
	Via        []string       `yaml:"via"`
	Policy     CheckPolicy    `yaml:"policy"`
}

type CheckPolicy struct {
	Attempts, Repeat                     *uint32
	Delay, Eventually, Interval, Timeout Duration
	StableFor                            Duration `yaml:"stable_for"`
}

// LoadFile reads and validates one watch YAML config.
func LoadFile(path string) (harness.Plan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return harness.Plan{}, err
	}
	return Parse(data)
}

// Parse compiles device-free YAML input into the common authoring Plan.
func Parse(data []byte) (harness.Plan, error) {
	var cfg Config
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return harness.Plan{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return harness.Plan{}, fmt.Errorf("exactly one YAML document is required")
	}
	return cfg.Plan()
}

// Plan validates cfg, applies defaults, and returns an executable watch plan.
func (cfg Config) Plan() (harness.Plan, error) {
	if cfg.Version != 0 && cfg.Version != 1 {
		return harness.Plan{}, fmt.Errorf("unsupported watch config version %d", cfg.Version)
	}
	if len(cfg.Targets) == 0 {
		return harness.Plan{}, fmt.Errorf("watch config must define at least one target")
	}
	if cfg.RoundInterval.Duration < 0 {
		return harness.Plan{}, fmt.Errorf("negative round interval")
	}
	plan := harness.Plan{Name: firstNonEmpty(cfg.Name, "dropcheck-watch"), RoundInterval: cfg.RoundInterval.Duration}
	for i, target := range cfg.Targets {
		target = applyTargetDefaults(target, cfg.Defaults)
		target = applyConfigVars(target, cfg.Vars)
		if strings.TrimSpace(target.SSID) == "" {
			return harness.Plan{}, fmt.Errorf("targets[%d] must set ssid", i)
		}
		if err := normalizeTargetMacRotation(&target, i); err != nil {
			return harness.Plan{}, err
		}
		if target.Name == "" {
			target.Name = targetDisplayName(target)
		}
		target.ShortName = strings.TrimSpace(target.ShortName)
		target.Agent = strings.TrimSpace(target.Agent)
		resolvedTarget, err := resolveTargetVars(target)
		if err != nil {
			return harness.Plan{}, fmt.Errorf("targets[%d] vars: %w", i, err)
		}
		target = resolvedTarget
		network := harness.WiFi(target.DisplayName()).SSID(target.SSID).BSSID(target.BSSID).Band(target.Band).Security(target.Security).MacRandomization(target.MacRandomization).
			ConnectTimeout(target.ConnectTimeout.Duration).WaitTimeout(target.WaitTimeout.Duration).RequireIP(target.requireIP()).RequireValidated(target.requireValidated()).
			DisconnectAfter(target.disconnectAfter()).ForgetAfter(target.forgetAfter()).Agent(target.Agent).ShortName(target.ShortName).MACRotation(target.MacRotation).
			ConnectPolicy(harness.Policy{Attempts: 4}).WaitPolicy(harness.Policy{Attempts: 4})
		if target.Passphrase != "" && target.PassphraseEnv != "" {
			return harness.Plan{}, fmt.Errorf("target has both passphrase and passphrase_env")
		}
		if target.PassphraseEnv != "" {
			network = network.PSKEnv(target.PassphraseEnv)
		} else {
			network = network.PSK(target.Passphrase)
		}
		if target.WaitConnected != nil {
			network = network.WaitConnected(*target.WaitConnected)
		}
		checks := append(append([]Check(nil), cfg.Checks...), target.Checks...)
		for j, check := range checks {
			resolved, resolveErr := resolveCheckForTarget(check, target)
			if resolveErr != nil {
				return harness.Plan{}, fmt.Errorf("target %d check %d: %w", i, j, resolveErr)
			}
			id := fmt.Sprintf("check/%d", j)
			if j >= len(cfg.Checks) {
				id = fmt.Sprintf("target/%d/check/%d", i, j-len(cfg.Checks))
			}
			compiled, compileErr := compileCheck(resolved, target, id)
			if compileErr != nil {
				return harness.Plan{}, fmt.Errorf("target %d check %d: %w", i, j, compileErr)
			}
			network = network.Checks(compiled)
		}
		plan.Networks = append(plan.Networks, network)
	}
	return plan, nil
}

func applyTargetDefaults(target Target, defaults TargetDefaults) Target {
	if target.Agent == "" {
		target.Agent = defaults.Agent
	}
	if target.Passphrase == "" {
		target.Passphrase = defaults.Passphrase
	}
	if target.PassphraseEnv == "" {
		target.PassphraseEnv = defaults.PassphraseEnv
	}
	if target.Security == "" {
		target.Security = defaults.Security
	}
	if target.MacRandomization == "" {
		target.MacRandomization = defaults.MacRandomization
	}
	if target.MacRotation == "" {
		target.MacRotation = defaults.MacRotation
	}
	if target.ConnectTimeout.Duration == 0 {
		target.ConnectTimeout = defaults.ConnectTimeout
	}
	if target.WaitTimeout.Duration == 0 {
		target.WaitTimeout = defaults.WaitTimeout
	}
	if target.RequireIP == nil {
		target.RequireIP = defaults.RequireIP
	}
	if target.RequireValidated == nil {
		target.RequireValidated = defaults.RequireValidated
	}
	if target.DisconnectAfter == nil {
		target.DisconnectAfter = defaults.DisconnectAfter
	}
	if target.ForgetAfter == nil {
		target.ForgetAfter = defaults.ForgetAfter
	}
	return target
}

func applyConfigVars(target Target, defaults map[string]string) Target {
	if len(defaults) == 0 && len(target.Vars) == 0 {
		return target
	}
	merged := make(map[string]string, len(defaults)+len(target.Vars))
	for key, value := range defaults {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		merged[key] = strings.TrimSpace(value)
	}
	for key, value := range target.Vars {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		merged[key] = strings.TrimSpace(value)
	}
	if len(merged) == 0 {
		target.Vars = nil
		return target
	}
	target.Vars = merged
	return target
}

const (
	macRotationNone      = "none"
	macRotationPerTarget = "per_target"
	macRotationPerRound  = "per_round"
)

func normalizeTargetMacRotation(target *Target, index int) error {
	rotation, err := normalizeMacRotation(target.MacRotation)
	if err != nil {
		return fmt.Errorf("targets[%d] mac_rotation: %w", index, err)
	}
	target.MacRotation = rotation
	if rotation == macRotationNone {
		return nil
	}
	if strings.TrimSpace(target.MacRandomization) == "" {
		target.MacRandomization = "non-persistent"
	}
	target.MacRandomization = strings.ToLower(strings.TrimSpace(target.MacRandomization))
	if target.MacRandomization != "non-persistent" {
		return fmt.Errorf("targets[%d] mac_rotation %q requires mac_randomization: non-persistent", index, rotation)
	}
	return nil
}

func normalizeMacRotation(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "none":
		return macRotationNone, nil
	case "per_target", "per-target":
		return macRotationPerTarget, nil
	case "per_round", "per-round":
		return macRotationPerRound, nil
	default:
		return "", fmt.Errorf("unsupported value %q; use none, per_target, or per_round", value)
	}
}

func targetDisplayName(target Target) string {
	parts := []string{target.SSID}
	if target.BSSID != "" {
		parts = append(parts, target.BSSID)
	}
	if target.Band != "" {
		parts = append(parts, target.Band)
	}
	return strings.Join(parts, "/")
}

// DisplayName returns the configured target name, falling back to SSID/BSSID/band.
func (target Target) DisplayName() string {
	if strings.TrimSpace(target.Name) != "" {
		return strings.TrimSpace(target.Name)
	}
	return targetDisplayName(target)
}

func (target Target) requireIP() bool {
	if target.RequireIP == nil {
		return true
	}
	return *target.RequireIP
}

func (target Target) requireValidated() bool {
	return target.RequireValidated != nil && *target.RequireValidated
}

func (target Target) macRotation() string {
	rotation, err := normalizeMacRotation(target.MacRotation)
	if err != nil {
		return macRotationNone
	}
	return rotation
}

func (target Target) disconnectAfter() bool {
	if target.macRotation() != macRotationNone {
		return true
	}
	if target.DisconnectAfter == nil {
		return true
	}
	return *target.DisconnectAfter
}

func (target Target) forgetAfter() bool {
	if target.macRotation() == macRotationPerTarget {
		return true
	}
	return target.ForgetAfter != nil && *target.ForgetAfter
}

// DisplayName returns the configured check name, falling back to its type.
func (check Check) DisplayName() string {
	if strings.TrimSpace(check.Name) != "" {
		return strings.TrimSpace(check.Name)
	}
	return strings.TrimSpace(check.Type)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
