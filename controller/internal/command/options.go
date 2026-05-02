package command

// Options describes controller-local behavior attached to an Operation.
//
// These options are intentionally kept outside controlpb.RunCommand because
// they affect presentation or validation in the controller rather than work the
// Android agent performs.
type Options struct {
	// TracerouteRequiredHops lists hop hostnames or addresses that should appear
	// in rendered traceroute output.
	TracerouteRequiredHops []string
}
