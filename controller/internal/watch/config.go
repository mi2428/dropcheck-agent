package watch

import (
	"fmt"
	"os"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// Config is the YAML surface consumed by `dropcheck watch -c`.
type Config struct {
	Version       int            `yaml:"version"`
	Name          string         `yaml:"name"`
	RoundInterval Duration       `yaml:"round_interval"`
	Defaults      TargetDefaults `yaml:"defaults"`
	Targets       []Target       `yaml:"targets"`
	Checks        []Check        `yaml:"checks"`
}

type TargetDefaults struct {
	Passphrase       string   `yaml:"passphrase"`
	PassphraseEnv    string   `yaml:"passphrase_env"`
	Security         string   `yaml:"security"`
	MacRandomization string   `yaml:"mac_randomization"`
	ConnectTimeout   Duration `yaml:"connect_timeout"`
	WaitTimeout      Duration `yaml:"wait_timeout"`
	RequireIP        *bool    `yaml:"require_ip"`
	RequireValidated *bool    `yaml:"require_validated"`
	DisconnectAfter  *bool    `yaml:"disconnect_after"`
	ForgetAfter      *bool    `yaml:"forget_after"`
}

type Target struct {
	Name             string   `yaml:"name"`
	SSID             string   `yaml:"ssid"`
	BSSID            string   `yaml:"bssid"`
	Band             string   `yaml:"band"`
	Passphrase       string   `yaml:"passphrase"`
	PassphraseEnv    string   `yaml:"passphrase_env"`
	Security         string   `yaml:"security"`
	MacRandomization string   `yaml:"mac_randomization"`
	ConnectTimeout   Duration `yaml:"connect_timeout"`
	WaitTimeout      Duration `yaml:"wait_timeout"`
	RequireIP        *bool    `yaml:"require_ip"`
	RequireValidated *bool    `yaml:"require_validated"`
	DisconnectAfter  *bool    `yaml:"disconnect_after"`
	ForgetAfter      *bool    `yaml:"forget_after"`
}

type Check struct {
	Name           string         `yaml:"name"`
	Type           string         `yaml:"type"`
	Host           string         `yaml:"host"`
	Count          uint32         `yaml:"count"`
	SizeBytes      uint32         `yaml:"size_bytes"`
	MaxHops        uint32         `yaml:"max_hops"`
	MinMTU         uint32         `yaml:"min_mtu"`
	MaxMTU         uint32         `yaml:"max_mtu"`
	Query          string         `yaml:"query"`
	Record         string         `yaml:"record"`
	URL            string         `yaml:"url"`
	Status         uint32         `yaml:"status"`
	Family         string         `yaml:"family"`
	ScanTarget     string         `yaml:"scan_target"`
	Band           string         `yaml:"band"`
	Timeout        Duration       `yaml:"timeout"`
	Expect         map[string]any `yaml:"expect"`
	compiledExpect []Matcher
}

type Plan struct {
	Name          string
	RoundInterval time.Duration
	Targets       []Target
	Checks        []Check
}

// LoadFile reads and validates one watch YAML config.
func LoadFile(path string) (Plan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Plan{}, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Plan{}, err
	}
	return cfg.Plan()
}

func (cfg Config) Plan() (Plan, error) {
	if cfg.Version != 0 && cfg.Version != 1 {
		return Plan{}, fmt.Errorf("unsupported watch config version %d", cfg.Version)
	}
	if len(cfg.Targets) == 0 {
		return Plan{}, fmt.Errorf("watch config must define at least one target")
	}
	if len(cfg.Checks) == 0 {
		return Plan{}, fmt.Errorf("watch config must define at least one check")
	}
	targets := make([]Target, 0, len(cfg.Targets))
	for i, target := range cfg.Targets {
		target = applyTargetDefaults(target, cfg.Defaults)
		if strings.TrimSpace(target.SSID) == "" {
			return Plan{}, fmt.Errorf("targets[%d] must set ssid", i)
		}
		if target.Name == "" {
			target.Name = targetDisplayName(target)
		}
		targets = append(targets, target)
	}
	checks := make([]Check, 0, len(cfg.Checks))
	for i, check := range cfg.Checks {
		check.Type = strings.TrimSpace(check.Type)
		if check.Type == "" {
			return Plan{}, fmt.Errorf("checks[%d] must set type", i)
		}
		if check.Name == "" {
			check.Name = check.Type
		}
		matchers, err := compileMatchers(check.Expect)
		if err != nil {
			return Plan{}, fmt.Errorf("checks[%d] %s expect: %w", i, check.Name, err)
		}
		check.compiledExpect = matchers
		checks = append(checks, check)
	}
	return Plan{
		Name:          firstNonEmpty(cfg.Name, "dropcheck-watch"),
		RoundInterval: cfg.RoundInterval.Duration,
		Targets:       targets,
		Checks:        checks,
	}, nil
}

func applyTargetDefaults(target Target, defaults TargetDefaults) Target {
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

func (target Target) disconnectAfter() bool {
	if target.DisconnectAfter == nil {
		return true
	}
	return *target.DisconnectAfter
}

func (target Target) forgetAfter() bool {
	return target.ForgetAfter != nil && *target.ForgetAfter
}

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
