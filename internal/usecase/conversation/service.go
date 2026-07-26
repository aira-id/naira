// Package conversation orchestrates the core wake-word-gated loop and the
// gated EXECUTE_AGENT dispatch flow (RFC.md#sequence). It depends only on
// domain ports so STT/LLM/TTS/Agent implementations (CGo bindings today,
// stubs in the meantime) are swappable.
package conversation

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"naira/internal/domain"
	"naira/internal/idgen"
	statesvc "naira/internal/usecase/state"
)

// Engines bundles the pluggable subsystem ports the orchestrator needs.
// Sound is optional (nil disables audio cues entirely).
type Engines struct {
	STT          domain.STTEngine
	LLM          domain.LLMEngine
	TTS          domain.TTSEngine
	Agent        domain.AgentEngine
	UI           domain.UIPublisher
	Connectivity domain.ConnectivityChecker
	Auth         domain.AuthChecker
	Sound        domain.SoundBoard
}

type Service struct {
	engines  Engines
	state    *statesvc.Service
	gamesDir string

	mu         sync.Mutex
	turnCancel context.CancelFunc // set while a turn (LLM infer + TTS) is in flight
}

// New wires an orchestrator. gamesDir is the root under which EXECUTE_AGENT
// output is sandboxed (RFC.md#apis External Subprocess Contract).
func New(engines Engines, state *statesvc.Service, gamesDir string) *Service {
	return &Service{engines: engines, state: state, gamesDir: gamesDir}
}

// Interrupt cancels the in-flight turn (LLM generation + TTS playback), if
// any — the tap-to-interrupt gesture from the face UI. Safe to call at any
// time, including when no turn is running (no-op).
func (s *Service) Interrupt() {
	s.mu.Lock()
	cancel := s.turnCancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// HandleUtterance runs one already-transcribed turn through the LLM, tag
// router, and TTS, dispatching EXECUTE_AGENT/OPEN_BROWSER as gated actions.
// Wake-word detection and STT happen upstream (RFC.md#sequence Core
// Conversation Flow) — by the time text reaches here, raw audio has already
// been discarded.
func (s *Service) HandleUtterance(ctx context.Context, sessionID, transcript string) error {
	// turnCtx is cancelled by Interrupt() (tap-to-interrupt on the face UI)
	// — LLM inference, TTS playback, and sound cues all key off it so a tap
	// stops generation and kills any in-progress subprocess (piper/aplay)
	// immediately, rather than only suppressing future sentences. UI state
	// resets below intentionally use the outer ctx, not turnCtx, so they
	// still take effect after an interrupt.
	turnCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.turnCancel = cancel
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.turnCancel = nil
		s.mu.Unlock()
		cancel()
	}()

	if s.engines.Sound != nil {
		go func() { _ = s.engines.Sound.Play(turnCtx, domain.SoundAck) }()
	}
	_ = s.engines.UI.SetState(ctx, domain.ExpressionThinking)

	stopThinking := s.startThinkingLoop(turnCtx)

	var sb strings.Builder
	seq := 0
	out, err := s.engines.LLM.Infer(turnCtx, transcript, func(sentence string) {
		stopThinking()
		sb.WriteString(sentence)
		_ = s.engines.UI.SpeakChunk(ctx, sentence, seq)
		seq++
		if err := s.engines.TTS.Speak(turnCtx, sentence); err != nil {
			// Best-effort: a dropped sentence shouldn't abort the turn.
			return
		}
	})
	stopThinking()
	if err != nil {
		if turnCtx.Err() != nil {
			// Interrupted, not a real failure — reset and stop quietly.
			_ = s.engines.UI.SetState(ctx, domain.ExpressionIdle)
			return nil
		}
		return fmt.Errorf("llm infer: %w", err)
	}

	_ = s.engines.UI.SetState(ctx, out.Expression)
	_ = s.engines.UI.SetState(ctx, domain.ExpressionSpeaking)

	switch out.Action {
	case domain.ActionExecuteAgent:
		return s.dispatchAgent(turnCtx, sessionID, out)
	case domain.ActionOpenBrowser:
		return s.dispatchOpenBrowser(turnCtx, out)
	case domain.ActionNone:
		// Nothing further to do — spoken response already streamed to TTS.
	}

	_ = s.engines.UI.SetState(ctx, domain.ExpressionIdle)
	return nil
}

// startThinkingLoop plays domain.SoundThinking on repeat (one clip at a
// time, blocking between repeats) until the returned stop func is called —
// wired to fire on the LLM's first streamed sentence, so the hum only fills
// the gap while the model is still generating (be-more-agent's
// thinking_sound_active pattern). Safe to call stop multiple times or when
// Sound is nil.
func (s *Service) startThinkingLoop(ctx context.Context) func() {
	if s.engines.Sound == nil {
		return func() {}
	}

	stop := make(chan struct{})
	done := make(chan struct{})
	var once sync.Once

	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			default:
			}
			_ = s.engines.Sound.Play(ctx, domain.SoundThinking)
		}
	}()

	return func() {
		once.Do(func() { close(stop) })
		<-done
	}
}

var nameSanitizer = regexp.MustCompile(`[^a-z0-9-]+`)

func sanitizeName(raw string) string {
	name := strings.ToLower(strings.TrimSpace(raw))
	name = nameSanitizer.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	if name == "" {
		name = "app"
	}
	return name
}

// speak pushes text as a one-off caption (seq 0 — these are standalone
// fallback messages, not part of a streamed multi-sentence turn) alongside
// TTS, for the single-shot fallback responses below.
func (s *Service) speak(ctx context.Context, text string) error {
	_ = s.engines.UI.SpeakChunk(ctx, text, 0)
	return s.engines.TTS.Speak(ctx, text)
}

// dispatchAgent implements the gated EXECUTE_AGENT flow: connectivity check,
// then auth check, then sandboxed subprocess dispatch — with a vocal
// fallback at either gate (RFC.md#sequence EXECUTE_AGENT Game Generation Flow).
func (s *Service) dispatchAgent(ctx context.Context, sessionID string, out domain.LLMOutput) error {
	jobID := idgen.New()
	_ = s.engines.UI.SetState(ctx, domain.ExpressionWorking)
	_ = s.engines.UI.SetWindowMode(ctx, true, 250, 250)

	if !s.engines.Connectivity.Online(ctx) {
		_ = s.engines.UI.AgentStatus(ctx, "OFFLINE_BLOCKED", jobID)
		return s.speak(ctx, "I need internet for that, and we're offline right now.")
	}

	if !s.engines.Auth.Authorized(ctx) {
		_ = s.engines.UI.AgentStatus(ctx, "UNAUTHORIZED", jobID)
		return s.speak(ctx, "I can't build apps yet — my agent isn't set up.")
	}

	name := sanitizeName(out.Text)
	_ = s.engines.UI.AgentStatus(ctx, "DISPATCHED", jobID)

	result, err := s.engines.Agent.Execute(ctx, domain.AgentJob{Name: name, PromptText: out.Text})
	if err != nil || result.Failed {
		_ = s.engines.UI.AgentStatus(ctx, "FAILED", jobID)
		reason := result.FailureReason
		if reason == "" && err != nil {
			reason = err.Error()
		}
		return s.speak(ctx, fmt.Sprintf("Sorry, I couldn't finish building that. (%s)", reason))
	}

	if _, saveErr := s.state.RegisterGeneratedApp(ctx, name, "game", result.IndexHTMLPath, out.Text); saveErr != nil {
		return fmt.Errorf("register generated app: %w", saveErr)
	}
	_ = s.state.RecordSkillUsage(ctx, sessionID, "game_generation")
	_ = s.engines.UI.AgentStatus(ctx, "DONE", jobID)
	return nil
}

func (s *Service) dispatchOpenBrowser(ctx context.Context, out domain.LLMOutput) error {
	if !s.engines.Connectivity.Online(ctx) {
		_ = s.engines.UI.SetState(ctx, domain.ExpressionSympathetic)
		return s.speak(ctx, "I can't open that right now — we're offline.")
	}
	_ = s.engines.UI.SetWindowMode(ctx, true, 250, 250)
	return nil
}

// SandboxPath returns the sandboxed output directory for a given app name
// (RFC.md#apis: filesystem writes restricted to /games/<name>/ only).
func (s *Service) SandboxPath(name string) string {
	return filepath.Join(s.gamesDir, sanitizeName(name))
}
