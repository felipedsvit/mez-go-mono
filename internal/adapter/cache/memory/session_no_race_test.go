//go:build !race

// Package memory tests live in session_test.go (race-tagged) and session_clock_test.go (no-race).
// The split keeps the go-leak detector from the long-lived reaper test.
package memory
