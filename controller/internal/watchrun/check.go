package watchrun

import "dropcheck/controller/internal/command"

// Check is a compiled YAML or built-in Shell operation and its evaluation policy.
type Check struct {
	Name         string
	SourceID     string
	Operation    command.Operation
	Expectations []Expectation
	Policy       Policy
	Required     bool
	Gateway      *GatewayPingOptions
}

type step struct {
	name         string
	operation    command.Operation
	expectations []Expectation
	policy       Policy
	required     bool
	sourceID     string
	gateway      *GatewayPingOptions
}
