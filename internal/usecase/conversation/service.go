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

	"naira/internal/domain"
	"naira/internal/idgen"
	statesvc "naira/internal/usecase/state"
)

// Engines bundles the pluggable subsystem ports the orchestrator needs.
type Engines struct {
	STT          domain.STTEngine
	LLM          domain.LLMEngine
	TTS          domain.TTSEngine
	Agent        domain.AgentEngine
	UI           domain.UIPublisher
	Connectivity domain.ConnectivityChecker
	Auth         domain.AuthChecker
}

type Service struct {
	engines  Engines
	state    *statesvc.Service
	gamesDir string
}

// New wires an orchestrator. gamesDir is the root under which EXECUTE_AGENT
// output is sandboxed (RFC.md#apis External Subprocess Contract).
func New(engines Engines, state *statesvc.Service, gamesDir string) *Service {
	return &Service{engines: engines, state: state, gamesDir: gamesDir}
}

// HandleUtterance runs one already-transcribed turn through the LLM, tag
// router, and TTS, dispatching EXECUTE_AGENT/OPEN_BROWSER as gated actions.
// Wake-word detection and STT happen upstream (RFC.md#sequence Core
// Conversation Flow) — by the time text reaches here, raw audio has already
// been discarded.
func (s *Service) HandleUtterance(ctx context.Context, sessionID, transcript string) error {
	_ = s.engines.UI.SetState(ctx, domain.ExpressionThinking)

	var sb strings.Builder
	out, err := s.engines.LLM.Infer(ctx, transcript, func(sentence string) {
		sb.WriteString(sentence)
		if err := s.engines.TTS.Speak(ctx, sentence); err != nil {
			// Best-effort: a dropped sentence shouldn't abort the turn.
			return
		}
	})
	if err != nil {
		return fmt.Errorf("llm infer: %w", err)
	}

	_ = s.engines.UI.SetState(ctx, out.Expression)
	_ = s.engines.UI.SetState(ctx, domain.ExpressionSpeaking)

	switch out.Action {
	case domain.ActionExecuteAgent:
		return s.dispatchAgent(ctx, sessionID, out)
	case domain.ActionOpenBrowser:
		return s.dispatchOpenBrowser(ctx, out)
	case domain.ActionNone:
		// Nothing further to do — spoken response already streamed to TTS.
	}

	_ = s.engines.UI.SetState(ctx, domain.ExpressionIdle)
	return nil
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

// dispatchAgent implements the gated EXECUTE_AGENT flow: connectivity check,
// then auth check, then sandboxed subprocess dispatch — with a vocal
// fallback at either gate (RFC.md#sequence EXECUTE_AGENT Game Generation Flow).
func (s *Service) dispatchAgent(ctx context.Context, sessionID string, out domain.LLMOutput) error {
	jobID := idgen.New()
	_ = s.engines.UI.SetState(ctx, domain.ExpressionWorking)
	_ = s.engines.UI.SetWindowMode(ctx, true, 250, 250)

	if !s.engines.Connectivity.Online(ctx) {
		_ = s.engines.UI.AgentStatus(ctx, "OFFLINE_BLOCKED", jobID)
		return s.engines.TTS.Speak(ctx, "I need internet for that, and we're offline right now.")
	}

	if !s.engines.Auth.Authorized(ctx) {
		_ = s.engines.UI.AgentStatus(ctx, "UNAUTHORIZED", jobID)
		return s.engines.TTS.Speak(ctx, "I can't build apps yet — my agent isn't set up.")
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
		return s.engines.TTS.Speak(ctx, fmt.Sprintf("Sorry, I couldn't finish building that. (%s)", reason))
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
		return s.engines.TTS.Speak(ctx, "I can't open that right now — we're offline.")
	}
	_ = s.engines.UI.SetWindowMode(ctx, true, 250, 250)
	return nil
}

// SandboxPath returns the sandboxed output directory for a given app name
// (RFC.md#apis: filesystem writes restricted to /games/<name>/ only).
func (s *Service) SandboxPath(name string) string {
	return filepath.Join(s.gamesDir, sanitizeName(name))
}
