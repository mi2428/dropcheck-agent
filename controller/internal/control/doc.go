// Package control hosts the gRPC control plane used by Android agents.
//
// A Server accepts authenticated agent sessions, tracks connected agents,
// dispatches RunCommand frames, delivers command results back to waiting
// callers, and forwards agent logs to the application layer. It does not start
// adb, choose devices, or render output; those responsibilities live in session
// and app.
package control
