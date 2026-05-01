// Package adb wraps the Android Debug Bridge commands used by dropcheck.
//
// The package intentionally keeps a narrow surface: it lists local Android
// devices, configures reverse port forwarding, starts the on-device agent
// service, and executes raw adb subcommands when the session layer needs a
// small escape hatch. Higher-level target discovery and session orchestration
// live outside this package so adb remains a process adapter rather than an
// application coordinator.
package adb
