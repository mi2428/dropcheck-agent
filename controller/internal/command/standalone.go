package command

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"dropcheck/controller/internal/controlpb"
)

const (
	defaultStandaloneRetention = 7 * 24 * time.Hour
	defaultStandaloneMaxBytes  = 512 * 1024 * 1024
)

// StandaloneEdit describes one JUNOS-style configuration edit applied by the
// Android agent to its persisted standalone config.
type StandaloneEdit struct {
	Action string
	Path   []string
	Value  string
}

// StandaloneListOptions describes a stored-run listing request.
type StandaloneListOptions struct {
	Limit         string
	IncludeSynced bool
}

// StandaloneRunOptions describes a one-shot standalone run request.
type StandaloneRunOptions struct {
	Festa string
	Save  bool
}

// StandaloneEditOperation builds an operation that applies one or more
// standalone configuration edits on the agent.
func StandaloneEditOperation(edits []StandaloneEdit) (Operation, error) {
	if len(edits) == 0 {
		return Operation{}, fmt.Errorf("standalone edit is required")
	}
	protoEdits := make([]*controlpb.StandaloneEdit, 0, len(edits))
	for _, edit := range edits {
		var action controlpb.StandaloneEdit_Action
		switch edit.Action {
		case "", "set":
			action = controlpb.StandaloneEdit_ACTION_SET
		case "delete":
			action = controlpb.StandaloneEdit_ACTION_DELETE
		default:
			return Operation{}, fmt.Errorf("unknown standalone edit action %q", edit.Action)
		}
		if len(edit.Path) == 0 {
			return Operation{}, fmt.Errorf("standalone edit path is required")
		}
		protoEdits = append(protoEdits, &controlpb.StandaloneEdit{
			Action: action,
			Path:   append([]string(nil), edit.Path...),
			Value:  edit.Value,
		})
	}
	return NewOperation("standalone.config.edit", &controlpb.RunCommand{
		Label: "standalone config edit",
		Command: &controlpb.RunCommand_EditStandaloneConfig{
			EditStandaloneConfig: &controlpb.EditStandaloneConfig{Edits: protoEdits},
		},
	}, Options{}), nil
}

// StandaloneConfigOperation builds an operation that fetches the persisted
// standalone configuration from the agent.
func StandaloneConfigOperation() Operation {
	return NewOperation("standalone.config", &controlpb.RunCommand{
		Label:   "standalone config",
		Command: &controlpb.RunCommand_GetStandaloneConfig{GetStandaloneConfig: &controlpb.GetStandaloneConfig{}},
	}, Options{})
}

// StandaloneStatusOperation builds an operation that fetches standalone runtime
// status and local archive counters.
func StandaloneStatusOperation() Operation {
	return NewOperation("standalone.status", &controlpb.RunCommand{
		Label:   "standalone status",
		Command: &controlpb.RunCommand_GetStandaloneStatus{GetStandaloneStatus: &controlpb.GetStandaloneStatus{}},
	}, Options{})
}

// StandaloneListRunsOperation builds an operation that lists archived
// standalone run summaries.
func StandaloneListRunsOperation(opts StandaloneListOptions) (Operation, error) {
	limit, err := parseOptionalUint32(opts.Limit, "limit", 0)
	if err != nil {
		return Operation{}, err
	}
	return NewOperation("standalone.runs", &controlpb.RunCommand{
		Label: "standalone runs",
		Command: &controlpb.RunCommand_ListStandaloneRuns{
			ListStandaloneRuns: &controlpb.ListStandaloneRuns{Limit: limit, IncludeSynced: opts.IncludeSynced},
		},
	}, Options{}), nil
}

// StandaloneRunOperation builds an operation that fetches one archived
// standalone run by ID.
func StandaloneRunOperation(runID string, markSynced bool) (Operation, error) {
	if strings.TrimSpace(runID) == "" {
		return Operation{}, fmt.Errorf("standalone run id is required")
	}
	return NewOperation("standalone.run", &controlpb.RunCommand{
		Label: "standalone run " + runID,
		Command: &controlpb.RunCommand_GetStandaloneRun{
			GetStandaloneRun: &controlpb.GetStandaloneRun{RunId: runID, MarkSynced: markSynced},
		},
	}, Options{}), nil
}

// StandaloneClearRunsOperation builds an operation that clears synced or all
// archived standalone runs.
func StandaloneClearRunsOperation(mode string) (Operation, error) {
	clear := &controlpb.ClearStandaloneRuns{}
	switch mode {
	case "", "synced":
		clear.SyncedOnly = true
	case "all":
		clear.All = true
	default:
		return Operation{}, fmt.Errorf("standalone clear mode must be synced or all")
	}
	return NewOperation("standalone.runs.clear", &controlpb.RunCommand{
		Label:   "standalone clear " + emptyLabel(mode, "synced"),
		Command: &controlpb.RunCommand_ClearStandaloneRuns{ClearStandaloneRuns: clear},
	}, Options{}), nil
}

// StandaloneRunOnceOperation builds an operation that runs one enabled
// standalone festa immediately.
func StandaloneRunOnceOperation(opts StandaloneRunOptions) (Operation, error) {
	return NewOperation("standalone.run.once", &controlpb.RunCommand{
		Label: "standalone run once",
		Command: &controlpb.RunCommand_RunStandaloneOnce{
			RunStandaloneOnce: &controlpb.RunStandaloneOnce{Festa: opts.Festa, Save: opts.Save},
		},
	}, Options{}), nil
}

// StandaloneSetBoolEdit returns a boolean standalone config set edit.
func StandaloneSetBoolEdit(path []string, value bool) StandaloneEdit {
	return StandaloneEdit{Action: "set", Path: path, Value: strconv.FormatBool(value)}
}

// StandaloneSetStringEdit returns a string standalone config set edit.
func StandaloneSetStringEdit(path []string, value string) StandaloneEdit {
	return StandaloneEdit{Action: "set", Path: path, Value: value}
}

// StandaloneSetMillisEdit returns a standalone config set edit whose value is
// normalized to milliseconds.
func StandaloneSetMillisEdit(path []string, value string, fallback time.Duration) (StandaloneEdit, error) {
	ms, err := parseMillisToken(value, path[len(path)-1], fallback)
	if err != nil {
		return StandaloneEdit{}, err
	}
	return StandaloneEdit{Action: "set", Path: path, Value: strconv.FormatUint(uint64(ms), 10)}, nil
}

// StandaloneSetBytesEdit returns a standalone config set edit whose value is
// normalized to bytes.
func StandaloneSetBytesEdit(path []string, value string, fallback uint64) (StandaloneEdit, error) {
	bytes, err := parseByteToken(value, fallback)
	if err != nil {
		return StandaloneEdit{}, err
	}
	return StandaloneEdit{Action: "set", Path: path, Value: strconv.FormatUint(bytes, 10)}, nil
}

// StandaloneDeleteEdit returns a standalone config delete edit.
func StandaloneDeleteEdit(path []string) StandaloneEdit {
	return StandaloneEdit{Action: "delete", Path: path}
}

// DefaultStandaloneRetention returns the controller default retention window
// for synced standalone run archives.
func DefaultStandaloneRetention() time.Duration {
	return defaultStandaloneRetention
}

// DefaultStandaloneMaxBytes returns the controller default storage budget for
// standalone run archives.
func DefaultStandaloneMaxBytes() uint64 {
	return defaultStandaloneMaxBytes
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
	if daysValue, ok := strings.CutSuffix(value, "d"); ok {
		days, err := strconv.ParseUint(daysValue, 10, 32)
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
	if unit != 0 && n > ^uint64(0)/unit {
		return 0, fmt.Errorf("max-size is outside uint64 byte range")
	}
	return n * unit, nil
}

func emptyLabel(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
