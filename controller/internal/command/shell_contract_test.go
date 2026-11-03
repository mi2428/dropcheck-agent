package command

import (
	"os"
	"strings"
	"testing"

	"dropcheck/controller/internal/controlpb"
)

func TestPortableCommandContract(t *testing.T) {
	data, err := os.ReadFile("../../../shell/command_cases.tsv")
	if err != nil {
		t.Fatal(err)
	}
	covered := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")[1:] {
		cols := strings.Split(line, "\t")
		args, err := SplitArgs(cols[1])
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := ParseShellTokens(args)
		if err != nil || parsed.Path != cols[0] {
			t.Errorf("%s: %+v %v", cols[0], parsed, err)
		}
		covered[cols[0]] = true
	}
	for _, spec := range portableGrammar {
		if !covered[spec.Path] {
			t.Errorf("missing contract case: %s", spec.Path)
		}
	}
	invalid, err := os.ReadFile("../../../shell/invalid_cases.tsv")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSuffix(string(invalid), "\n"), "\n")[1:] {
		args, err := SplitArgs(line)
		var parsed Parsed
		if err == nil {
			parsed, err = ParseShellTokens(args)
		}
		if err == nil && parsed.Rejection == "" {
			t.Errorf("accepted invalid common input: %s", line)
		}
	}
}

func TestNetworkIdentityBoundary(t *testing.T) {
	for _, target := range []string{"default", "42"} {
		op, err := NetworkOperation(target)
		if err != nil || ValidateOperation(op) != nil {
			t.Fatal(target, err)
		}
	}
	for _, target := range []string{"0", "01", "-1", "stale", "2147483648"} {
		if _, err := NetworkOperation(target); err == nil {
			t.Errorf("accepted %s", target)
		}
	}
	for _, query := range []*controlpb.GetNetwork{{}, {DefaultNetwork: true, NetworkId: "42"}, {NetworkId: "01"}} {
		op := NewOperation("network.show", &controlpb.RunCommand{Command: &controlpb.RunCommand_GetNetwork{GetNetwork: query}}, Options{})
		if ValidateOperation(op) == nil {
			t.Fatal("accepted inconsistent selector")
		}
	}
}
