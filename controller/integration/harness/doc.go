// Package harness contains real-device Dropcheck Harness smoke scenarios.
//
// Test files in this package are guarded by the "harness" build tag so normal
// unit-test runs do not connect to Wi-Fi networks or require Android devices.
// The legacy "festival" build tag is still accepted for compatibility.
//
// Smoke tests are configured by DROPCHECK_HARNESS_* environment variables, with
// DROPCHECK_FESTIVAL_* accepted as compatibility fallbacks.
package harness
