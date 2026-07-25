// Package engine provides stand-in implementations of the STT/LLM/TTS/Agent
// ports, wired by `naira run` until the real CGo bindings (whisper.cpp,
// llama.cpp, Piper ONNX Runtime — see RFC.md#architecture--tech-stack) are
// implemented in Phase 1. They let the orchestrator, CLI, and state layer be
// built, wired, and tested end-to-end today.
package engine

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"naira/internal/domain"
)

// StubSTT echoes back a canned transcript; a real implementation calls into
// whisper.cpp on the in-memory PCM buffer and never persists it to disk.
type StubSTT struct{}

func (StubSTT) Transcribe(ctx context.Context, pcm []byte) (string, error) {
	return "", fmt.Errorf("STT not implemented: whisper.cpp CGo binding pending (RFC.md Phase 1)")
}

// StubLLM always answers NONE/IDLE so the orchestrator loop is exercisable
// without llama.cpp wired in yet.
type StubLLM struct{}

func (StubLLM) Infer(ctx context.Context, prompt string, onSentence func(string)) (domain.LLMOutput, error) {
	text := fmt.Sprintf("I heard: %s", strings.TrimSpace(prompt))
	if onSentence != nil {
		onSentence(text)
	}
	return domain.LLMOutput{Expression: domain.ExpressionIdle, Action: domain.ActionNone, Text: text}, nil
}

// StubTTS logs instead of producing audio.
type StubTTS struct{}

func (StubTTS) Speak(ctx context.Context, text string) error {
	slog.Info("tts (stub)", "text", text)
	return nil
}

// StubAgent refuses to build anything until the sandbox enforcement
// mechanism is decided (RFC.md §5 Concerns: sandbox enforcement undecided).
type StubAgent struct{}

func (StubAgent) Execute(ctx context.Context, job domain.AgentJob) (domain.AgentResult, error) {
	return domain.AgentResult{Failed: true, FailureReason: "agent execution not implemented"}, nil
}

// StubAuth reports never-authorized until key storage is wired
// (RFC.md Security Implications: key/credential storage).
type StubAuth struct{}

func (StubAuth) Authorized(ctx context.Context) bool { return false }

// StubUI logs IPC messages instead of pushing them over WebSocket.
type StubUI struct{}

func (StubUI) SetState(ctx context.Context, state domain.ExpressionTag) error {
	slog.Info("ui state_change (stub)", "state", state)
	return nil
}

func (StubUI) SetWindowMode(ctx context.Context, floating bool, w, h int) error {
	mode := "FULLSCREEN"
	if floating {
		mode = "FLOATING"
	}
	slog.Info("ui window_mode (stub)", "mode", mode, "w", w, "h", h)
	return nil
}

func (StubUI) MouthAmplitude(ctx context.Context, amplitude float64, tsMillis int64) error {
	return nil
}

func (StubUI) AgentStatus(ctx context.Context, status, jobID string) error {
	slog.Info("ui agent_status (stub)", "status", status, "job_id", jobID)
	return nil
}
