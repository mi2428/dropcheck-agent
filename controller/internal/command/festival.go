package command

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"dropcheck/controller/internal/controlpb"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	defaultFestivalInterval  = 30 * time.Second
	defaultFestivalRetention = 7 * 24 * time.Hour
	defaultFestivalMaxBytes  = 512 * 1024 * 1024
)

// FestivalConfigOptions describes a persistent Dropcheck Festival standalone
// configuration update.
type FestivalConfigOptions struct {
	// Enabled controls whether the Android agent should keep measuring by itself.
	Enabled bool
	// PlanPath is a protojson FestivalPlan file loaded by the controller.
	PlanPath string
	// Interval is the delay between standalone runs. Empty uses a safe default.
	Interval string
	// Retention is the synced-result retention window. Empty uses a safe default.
	Retention string
	// MaxSize is the result-store budget. Empty uses a safe default.
	MaxSize string
}

// FestivalListOptions describes a stored-run listing request.
type FestivalListOptions struct {
	// Limit caps the returned summaries. Empty lets the agent choose a default.
	Limit string
	// IncludeSynced includes runs that have already been acknowledged by a sync.
	IncludeSynced bool
}

// FestivalSetConfigOperation builds a persistent standalone configuration update.
func FestivalSetConfigOperation(opts FestivalConfigOptions) (Operation, error) {
	config := &controlpb.FestivalConfig{Enabled: opts.Enabled}
	if opts.Enabled {
		plan, err := loadFestivalPlan(opts.PlanPath)
		if err != nil {
			return Operation{}, err
		}
		intervalMs, err := parseMillisToken(opts.Interval, "interval", defaultFestivalInterval)
		if err != nil {
			return Operation{}, err
		}
		retentionMs, err := parseMillisToken(opts.Retention, "retention", defaultFestivalRetention)
		if err != nil {
			return Operation{}, err
		}
		maxBytes, err := parseByteToken(opts.MaxSize, defaultFestivalMaxBytes)
		if err != nil {
			return Operation{}, err
		}
		config.Plan = plan
		config.IntervalMs = intervalMs
		config.RetentionMs = retentionMs
		config.MaxBytes = maxBytes
	}
	label := "festival standalone disabled"
	if opts.Enabled {
		label = "festival standalone enabled"
	}
	return NewOperation("festival.config.set", &controlpb.RunCommand{
		Label: label,
		Command: &controlpb.RunCommand_SetFestivalConfig{
			SetFestivalConfig: &controlpb.SetFestivalConfig{Config: config},
		},
	}, Options{}), nil
}

// FestivalConfigOperation retrieves the persistent standalone configuration.
func FestivalConfigOperation() Operation {
	return NewOperation("festival.config", &controlpb.RunCommand{
		Label:   "festival config",
		Command: &controlpb.RunCommand_GetFestivalConfig{GetFestivalConfig: &controlpb.GetFestivalConfig{}},
	}, Options{})
}

// FestivalStatusOperation retrieves the live standalone runner status.
func FestivalStatusOperation() Operation {
	return NewOperation("festival.status", &controlpb.RunCommand{
		Label:   "festival status",
		Command: &controlpb.RunCommand_GetFestivalStatus{GetFestivalStatus: &controlpb.GetFestivalStatus{}},
	}, Options{})
}

// FestivalListRunsOperation lists stored standalone measurement runs.
func FestivalListRunsOperation(opts FestivalListOptions) (Operation, error) {
	limit, err := parseOptionalUint32(opts.Limit, "limit", 0)
	if err != nil {
		return Operation{}, err
	}
	return NewOperation("festival.runs", &controlpb.RunCommand{
		Label: "festival runs",
		Command: &controlpb.RunCommand_ListFestivalRuns{
			ListFestivalRuns: &controlpb.ListFestivalRuns{Limit: limit, IncludeSynced: opts.IncludeSynced},
		},
	}, Options{}), nil
}

// FestivalRunOperation retrieves one stored measurement archive.
func FestivalRunOperation(runID string, markSynced bool) (Operation, error) {
	if strings.TrimSpace(runID) == "" {
		return Operation{}, fmt.Errorf("festival run id is required")
	}
	return NewOperation("festival.run", &controlpb.RunCommand{
		Label: "festival run " + runID,
		Command: &controlpb.RunCommand_GetFestivalRun{
			GetFestivalRun: &controlpb.GetFestivalRun{RunId: runID, MarkSynced: markSynced},
		},
	}, Options{}), nil
}

// FestivalClearRunsOperation removes stored run archives according to mode.
func FestivalClearRunsOperation(mode string) (Operation, error) {
	clear := &controlpb.ClearFestivalRuns{}
	switch mode {
	case "", "synced":
		clear.SyncedOnly = true
	case "all":
		clear.All = true
	default:
		return Operation{}, fmt.Errorf("festival clear mode must be synced or all")
	}
	return NewOperation("festival.runs.clear", &controlpb.RunCommand{
		Label:   "festival clear " + mode,
		Command: &controlpb.RunCommand_ClearFestivalRuns{ClearFestivalRuns: clear},
	}, Options{}), nil
}

// FestivalRunOnceOperation builds a one-shot measurement run from a protojson
// FestivalPlan file. The Android agent records measurements but does not
// evaluate expectations.
func FestivalRunOnceOperation(planPath string, save bool) (Operation, error) {
	plan, err := loadFestivalPlan(planPath)
	if err != nil {
		return Operation{}, err
	}
	return NewOperation("festival.run.once", &controlpb.RunCommand{
		Label: "festival run once",
		Command: &controlpb.RunCommand_RunFestivalOnce{
			RunFestivalOnce: &controlpb.RunFestivalOnce{Plan: plan, Save: save},
		},
	}, Options{}), nil
}

func loadFestivalPlan(path string) (*controlpb.FestivalPlan, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("festival plan path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read festival plan %q: %w", path, err)
	}
	var plan controlpb.FestivalPlan
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("parse festival plan %q: %w", path, err)
	}
	if len(plan.GetNetworks()) == 0 {
		return nil, fmt.Errorf("festival plan %q has no networks", path)
	}
	return &plan, nil
}

func parseMillisToken(value string, name string, fallback time.Duration) (uint32, error) {
	if value == "" {
		return uint32(fallback / time.Millisecond), nil
	}
	if n, err := strconv.ParseUint(value, 10, 32); err == nil {
		return uint32(n), nil
	}
	duration, err := parseDurationToken(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be milliseconds or duration: %w", name, err)
	}
	if duration < 0 || duration > time.Duration(^uint32(0))*time.Millisecond {
		return 0, fmt.Errorf("%s is outside uint32 millisecond range", name)
	}
	return uint32(duration / time.Millisecond), nil
}

func parseDurationToken(value string) (time.Duration, error) {
	if strings.HasSuffix(value, "d") {
		days, err := strconv.ParseUint(strings.TrimSuffix(value, "d"), 10, 32)
		if err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(value)
}

func parseByteToken(value string, fallback uint64) (uint64, error) {
	if value == "" {
		return fallback, nil
	}
	unit := uint64(1)
	number := value
	switch suffix := strings.ToLower(value[len(value)-1:]); suffix {
	case "k":
		unit = 1024
		number = value[:len(value)-1]
	case "m":
		unit = 1024 * 1024
		number = value[:len(value)-1]
	case "g":
		unit = 1024 * 1024 * 1024
		number = value[:len(value)-1]
	}
	n, err := strconv.ParseUint(number, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("max-size must be bytes or use k/m/g suffix: %w", err)
	}
	return n * unit, nil
}
