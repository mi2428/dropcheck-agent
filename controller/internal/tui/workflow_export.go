package tui

import (
	"fmt"
	"os"
	"path/filepath"

	"dropcheck/controller/internal/harness"
	"dropcheck/controller/internal/render"
)

// Export only a completed, already sanitized core Report; never retain a command or replay it.
func exportReportJSON(destination string, report harness.Report, overwrite bool) (string, error) {
	data, err := render.SafeReportJSON(report)
	if err != nil {
		return "", err
	}
	path, err := filepath.Abs(destination)
	if err != nil {
		return "", err
	}
	if overwrite {
		info, err := os.Lstat(path)
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("overwrite requires an existing regular file")
		}
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".dropcheck-report-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return "", err
	}
	_, err = file.Write(append(data, '\n'))
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	if overwrite {
		err = os.Rename(file.Name(), path)
	} else {
		err = os.Link(file.Name(), path) // Exclusive creation; never follow a preexisting symlink.
	}
	if err != nil {
		return "", err
	}
	return path, nil
}
