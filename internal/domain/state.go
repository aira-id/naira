package domain

// CurrentSchemaVersion is bumped whenever the State JSON shape changes.
// See RFC.md#local-state-storage for the migration contract.
const CurrentSchemaVersion = 1

// State is the full contents of ~/.naira/state.json.
// It holds runtime/behavioral state only — model identifiers/paths live in
// ModelsConfig (models.yaml), never here. No field may store raw or
// transcribed audio.
type State struct {
	SchemaVersion int             `json:"schema_version"`
	ParentConsent ParentConsent   `json:"parent_consent"`
	Config        RuntimeConfig   `json:"config"`
	Sessions      []Session       `json:"sessions"`
	ScreenTimeLog []ScreenTimeLog `json:"screen_time_log"`
	SkillUsage    []SkillUsage    `json:"skill_usage"`
	GeneratedApps []GeneratedApp  `json:"generated_apps"`
}

// NewState returns a fresh, unconsented state at the current schema version.
func NewState() *State {
	return &State{
		SchemaVersion: CurrentSchemaVersion,
		Config:        DefaultRuntimeConfig(),
		Sessions:      []Session{},
		ScreenTimeLog: []ScreenTimeLog{},
		SkillUsage:    []SkillUsage{},
		GeneratedApps: []GeneratedApp{},
	}
}

// ParentConsent gates first-run. The device must not leave setup mode until
// this is populated (RFC §3 Security Implications, §4 Rollout Strategy).
type ParentConsent struct {
	DisclosureVersion string `json:"disclosure_version"`
	AcceptedAt        string `json:"accepted_at"`
	DeviceID          string `json:"device_id"`
}

// Accepted reports whether the parent has completed the consent gate.
func (p ParentConsent) Accepted() bool {
	return p.AcceptedAt != "" && p.DisclosureVersion != ""
}

// RuntimeConfig holds wake word, TTS voice selection, thread overrides, and
// screen-time thresholds — settings that change via app UI/voice, not by
// editing a file. Does NOT store model identifiers/paths (see ModelsConfig).
type RuntimeConfig struct {
	WakeWord                   string `json:"wake_word"`
	TTSVoice                   string `json:"tts_voice"`
	ThreadOverride             *int   `json:"thread_override"`
	ScreenTimeThresholdMinutes int    `json:"screen_time_threshold_minutes"`
}

// DefaultRuntimeConfig matches the defaults documented in RFC.md.
func DefaultRuntimeConfig() RuntimeConfig {
	return RuntimeConfig{
		WakeWord:                   "hey naira",
		TTSVoice:                   "id_ID-news_tts-medium",
		ThreadOverride:             nil,
		ScreenTimeThresholdMinutes: 60,
	}
}

// Session is one wake-to-idle interaction window.
type Session struct {
	ID        string `json:"id"`
	StartedAt string `json:"started_at"`
	EndedAt   string `json:"ended_at,omitempty"`
	WakeCount int    `json:"wake_count"`
}

// ScreenTimeLog powers the [SLEEPY] screen-time-limit behavior
// (PRD §4.3 Skill 5).
type ScreenTimeLog struct {
	SessionID     string `json:"session_id"`
	Date          string `json:"date"`
	ActiveMinutes int    `json:"active_minutes"`
}

// SkillUsage is local-only analytics for the five companion skills, never
// transmitted off-device.
type SkillUsage struct {
	SessionID string `json:"session_id"`
	SkillName string `json:"skill_name"`
	InvokedAt string `json:"invoked_at"`
}

// GeneratedApp is the registry of what EXECUTE_AGENT has built, so the robot
// can reopen an app without regenerating it. Stores PromptText for
// reproducibility — never stores audio.
type GeneratedApp struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	AppType    string `json:"app_type"`
	FSPath     string `json:"fs_path"`
	PromptText string `json:"prompt_text"`
	CreatedAt  string `json:"created_at"`
}
