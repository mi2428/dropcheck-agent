package standaloneseed

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/watch"
)

const (
	liveFestaName   = "live"
	liveWatchUsage  = "usage: set standalone live watch <file> [<file>...]"
	maxUint32Millis = time.Duration(^uint32(0)) * time.Millisecond
)

// OperationFromSetArgs parses `set standalone live watch ...` and returns a
// standalone edit operation when the args match that surface.
func OperationFromSetArgs(args []string) (command.Operation, bool, error) {
	if len(args) == 0 || args[0] != "live" {
		return command.Operation{}, false, nil
	}
	if len(args) < 3 || args[1] != "watch" {
		return command.Operation{}, true, fmt.Errorf(liveWatchUsage)
	}
	edits, err := LiveWatchEdits(args[2:])
	if err != nil {
		return command.Operation{}, true, err
	}
	op, err := command.StandaloneEditOperation(edits)
	return op, true, err
}

// LiveWatchEdits replaces standalone festa `live` with Wi-Fi targets read from
// one or more watch configs.
func LiveWatchEdits(paths []string) ([]command.StandaloneEdit, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf(liveWatchUsage)
	}
	edits := []command.StandaloneEdit{
		command.StandaloneDeleteEdit([]string{"festa", liveFestaName}),
	}
	seenNames := make(map[string]string)
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			return nil, fmt.Errorf(liveWatchUsage)
		}
		plan, err := watch.LoadFile(path)
		if err != nil {
			return nil, fmt.Errorf("load watch config %q: %w", path, err)
		}
		for _, target := range plan.Targets {
			name := strings.TrimSpace(target.Name)
			if name == "" {
				return nil, fmt.Errorf("%q defines a target without a name", path)
			}
			folded := strings.ToLower(name)
			if previous, ok := seenNames[folded]; ok {
				return nil, fmt.Errorf("duplicate live wifi name %q in %q and %q", name, previous, path)
			}
			seenNames[folded] = path

			passphrase, err := resolvePassphrase(target)
			if err != nil {
				return nil, fmt.Errorf("%q target %q: %w", path, name, err)
			}
			timeoutMs, err := resolveTimeoutMillis(target.ConnectTimeout.Duration)
			if err != nil {
				return nil, fmt.Errorf("%q target %q: %w", path, name, err)
			}

			base := []string{"festa", liveFestaName, "wifi", name}
			edits = append(edits,
				command.StandaloneSetStringEdit(appendPath(base, "match", "essid"), target.SSID),
			)
			if target.BSSID != "" {
				edits = append(edits, command.StandaloneSetStringEdit(appendPath(base, "match", "bssid"), target.BSSID))
			}
			if passphrase != "" {
				edits = append(edits, command.StandaloneSetStringEdit(appendPath(base, "passphrase"), passphrase))
			}
			if target.Security != "" {
				edits = append(edits, command.StandaloneSetStringEdit(appendPath(base, "security"), target.Security))
			}
			if target.Band != "" {
				edits = append(edits, command.StandaloneSetStringEdit(appendPath(base, "band"), target.Band))
			}
			if target.MacRandomization != "" {
				edits = append(edits, command.StandaloneSetStringEdit(appendPath(base, "mac_randomization"), target.MacRandomization))
			}
			if timeoutMs != "" {
				edits = append(edits, command.StandaloneSetStringEdit(appendPath(base, "timeout_ms"), timeoutMs))
			}
		}
	}
	return edits, nil
}

func resolvePassphrase(target watch.Target) (string, error) {
	if target.Passphrase != "" {
		return target.Passphrase, nil
	}
	if target.PassphraseEnv == "" {
		return "", nil
	}
	value, ok := os.LookupEnv(target.PassphraseEnv)
	if !ok || value == "" {
		return "", fmt.Errorf("passphrase_env %s is not set or empty", target.PassphraseEnv)
	}
	return value, nil
}

func resolveTimeoutMillis(timeout time.Duration) (string, error) {
	if timeout <= 0 {
		return "", nil
	}
	if timeout > maxUint32Millis {
		return "", fmt.Errorf("connect_timeout exceeds uint32 milliseconds")
	}
	return strconv.FormatInt(timeout.Milliseconds(), 10), nil
}

func appendPath(base []string, extra ...string) []string {
	path := make([]string, 0, len(base)+len(extra))
	path = append(path, base...)
	path = append(path, extra...)
	return path
}
