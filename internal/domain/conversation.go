package domain

import (
	"fmt"
	"regexp"
)

// ExpressionTag drives the facial UI state machine (PRD §4.1).
type ExpressionTag string

const (
	ExpressionIdle        ExpressionTag = "IDLE"
	ExpressionListening   ExpressionTag = "LISTENING"
	ExpressionThinking    ExpressionTag = "THINKING"
	ExpressionSpeaking    ExpressionTag = "SPEAKING"
	ExpressionHappy       ExpressionTag = "HAPPY"
	ExpressionSurprised   ExpressionTag = "SURPRISED"
	ExpressionSympathetic ExpressionTag = "SYMPATHETIC"
	ExpressionWorking     ExpressionTag = "WORKING"
	ExpressionSleepy      ExpressionTag = "SLEEPY"
)

func (t ExpressionTag) Valid() bool {
	switch t {
	case ExpressionIdle, ExpressionListening, ExpressionThinking, ExpressionSpeaking,
		ExpressionHappy, ExpressionSurprised, ExpressionSympathetic, ExpressionWorking, ExpressionSleepy:
		return true
	}
	return false
}

// ActionTag is the system-action half of the tag routing pattern (PRD §4.2).
type ActionTag string

const (
	ActionNone         ActionTag = "NONE"
	ActionExecuteAgent ActionTag = "EXECUTE_AGENT"
	ActionOpenBrowser  ActionTag = "OPEN_BROWSER"
)

func (t ActionTag) Valid() bool {
	switch t {
	case ActionNone, ActionExecuteAgent, ActionOpenBrowser:
		return true
	}
	return false
}

// LLMOutput is the parsed contract: "[EXPRESSION_TAG] [ACTION_TAG] Spoken response string"
// (RFC.md#apis).
type LLMOutput struct {
	Expression ExpressionTag
	Action     ActionTag
	Text       string
}

var llmOutputPattern = regexp.MustCompile(`^\[([A-Z_]+)\]\s*\[([A-Z_]+)\]\s*(.*)$`)

// ParseLLMOutput parses raw LLM text into the tag contract. The Intent
// Parser / Tag Router must reject malformed output (missing tags, unknown
// enum value) rather than passing it through to TTS unfiltered
// (RFC.md#apis).
func ParseLLMOutput(raw string) (LLMOutput, error) {
	m := llmOutputPattern.FindStringSubmatch(raw)
	if m == nil {
		return LLMOutput{}, fmt.Errorf("malformed LLM output: missing [EXPRESSION_TAG][ACTION_TAG] prefix")
	}

	expr := ExpressionTag(m[1])
	action := ActionTag(m[2])

	if !expr.Valid() {
		return LLMOutput{}, fmt.Errorf("malformed LLM output: unknown expression tag %q", m[1])
	}
	if !action.Valid() {
		return LLMOutput{}, fmt.Errorf("malformed LLM output: unknown action tag %q", m[2])
	}

	return LLMOutput{Expression: expr, Action: action, Text: m[3]}, nil
}
