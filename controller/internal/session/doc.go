// Package session starts and stops a dropcheck control session for selected
// Android devices.
//
// A session owns the local gRPC server, adb reverse port forwarding, Android
// foreground-service launch, agent readiness wait, and cleanup. Open discovers
// ready devices; Start accepts an already selected target set.
package session
