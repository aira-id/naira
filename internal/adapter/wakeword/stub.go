// Package wakeword implements domain.WakeWordDetector. NoOp is a stub that
// never fires — the real engine (Porcupine, openWakeWord, ...) is an open
// decision (RFC.md §5 Concerns: wake-word engine unspecified). It exists so
// the capture/VAD/endpointing pipeline can be wired and tested without one.
package wakeword

// NoOp never detects a wake phrase.
type NoOp struct{}

func (NoOp) Detect(frame []byte) bool { return false }

// Always fires on every frame. Dev/testing only — lets the capture/VAD/
// endpointing pipeline be exercised end-to-end without a real wake-word
// engine; never use in a build a child interacts with.
type Always struct{}

func (Always) Detect(frame []byte) bool { return true }
