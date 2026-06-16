// Package session produces Claude Code (CC) and Codex session observations.
// Concrete functions only — no common Discoverer interface.
//
// TODO(wave1-b): the session field shapes are PROVISIONAL and will be finalized
// after the on-disk CC and Codex session formats are reverse-engineered.
package session

import "github.com/unsafe9/clutch/internal/model"

// Observe discovers active/recent CC and Codex sessions.
func Observe() ([]model.SessionObservation, error) {
	// TODO(wave1-b): locate and parse CC + Codex session files.
	panic("not implemented")
}
