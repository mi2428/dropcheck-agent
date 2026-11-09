package tuiconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mi2428/dropcheck-agent/controller/internal/tuirun"
)

func TestDocumentedTUIPlansValidate(t *testing.T) {
	t.Setenv("DROPCHECK_WIFI_PSK", "synthetic-passphrase")
	for _, path := range []string{"../../../README.md", "../../../examples/tui.yml"} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasSuffix(path, ".md") {
				_, section, found := strings.Cut(string(data), "### Continuous TUI dashboard\n")
				if !found {
					t.Fatal("README TUI section is missing")
				}
				section, _, _ = strings.Cut(section, "\n### ")
				section, _, _ = strings.Cut(section, "\n## ")
				_, block, found := strings.Cut(section, "```yaml\n")
				if !found {
					t.Fatal("README TUI YAML is missing")
				}
				block, _, found = strings.Cut(block, "\n```")
				if !found {
					t.Fatal("README TUI YAML fence is not closed")
				}
				data = []byte(block)
			}
			plan, err := Parse(data)
			if err != nil {
				t.Fatal(err)
			}
			if err := tuirun.Validate(plan); err != nil {
				t.Fatal(err)
			}
		})
	}
}
