package command

import (
	"strings"
	"testing"
)

func TestAdversarialQAEmptyQuotedLiteral(t *testing.T) {
	for _, input := range []string{`wifi forget ''`, `ping ''`, `http ''`, `dns ''`, `show wifi scan detail ''`} {
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
	if got := ShellSuggestions([]string{"check", "link"}); !strings.Contains(strings.Join(got, ","), "ssid") {
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
	for _, input := range []string{"wifi connect Lab passphrase '        '", "check link s Lab"} {
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
