package watch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mi2428/dropcheck-agent/controller/internal/watchrun"
)

func TestDocumentedWatchPlansValidate(t *testing.T) {
	t.Setenv("DROPCHECK_WIFI_PSK", "synthetic-passphrase")
	for _, path := range []string{"../../../README.md", "../../../examples/watch.yml"} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasSuffix(path, ".md") {
				_, section, found := strings.Cut(string(data), "### Continuous watch dashboard\n")
				if !found {
					t.Fatal("README Watch section is missing")
				}
				section, _, _ = strings.Cut(section, "\n### ")
				section, _, _ = strings.Cut(section, "\n## ")
				_, block, found := strings.Cut(section, "```yaml\n")
				if !found {
					t.Fatal("README Watch YAML is missing")
				}
				block, _, found = strings.Cut(block, "\n```")
				if !found {
					t.Fatal("README Watch YAML fence is not closed")
				}
				data = []byte(block)
			}
			plan, err := Parse(data)
			if err != nil {
				t.Fatal(err)
			}
			if err := watchrun.Validate(plan); err != nil {
				t.Fatal(err)
			}
		})
	}
}
