// Package session starts and stops a dropcheck control session for selected
// Android devices.
//
// A session owns the local gRPC server, adb reverse port forwarding, Android
// foreground-service launch, agent readiness wait, and cleanup. Device
// discovery is performed by the app package before Start is called so session
// startup can operate on an explicit target set.
package session
