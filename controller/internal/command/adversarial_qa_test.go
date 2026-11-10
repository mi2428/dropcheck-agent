package command

import (
	"strings"
	"testing"
)

func TestAdversarialQAEmptyQuotedLiteral(t *testing.T) {
	for _, input := range []string{`request wifi forget ssid ''`, `request ping ''`, `request http check ''`, `request http download ''`, `request dns resolve ''`, `show wifi scan ssid ''`} {
		tokens, err := SplitArgs(input)
		if err != nil {
			t.Fatal(err)
		}
		_, err = ParseShellTokens(tokens)
		if err == nil {
			t.Errorf("accepted empty required literal: %s", input)
		}
	}
}

func TestAdversarialQACheckCompletion(t *testing.T) {
	if got := ShellSuggestions([]string{"test", "link"}); !strings.Contains(strings.Join(got, ","), "ssid") {
		t.Errorf("SSID discovery missing: %v", got)
	}
}

func TestAdversarialQARenderDetailIntent(t *testing.T) {
	tokens, _ := SplitArgs("show network default")
	parsed, err := ParseShellTokens(tokens)
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.Operation.Options.Detail {
		t.Error("NetworkOperation's detail intent was overwritten by absent detail switch")
	}
}

func TestAdversarialQALiteralPSKAndPrefixBaseline(t *testing.T) {
	for _, input := range []string{"request wifi connect Lab passphrase '        '", "test link s Lab"} {
		tokens, err := SplitArgs(input)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := ParseShellTokens(tokens)
		if err != nil || parsed.Rejection != "" {
			t.Errorf("PC boundary rejected %q: %v %s", input, err, parsed.Rejection)
		}
	}
}
