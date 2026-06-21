// Package runner executes typed dropcheck operations against connected agents.
//
// The package is intentionally below app and harness. It knows how to turn a
// command.Operation into a controlpb.RunCommand, choose the controller timeout,
// assign a command ID, and call the control server, but it does not render
// output or decide which workflows to run.
package runner
