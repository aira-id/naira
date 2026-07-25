package domain

import "context"

// StateRepository persists/loads State to/from ~/.naira/state.json using
// the write-temp/fsync/rename pattern (RFC.md#local-state-storage).
type StateRepository interface {
	Load(ctx context.Context) (*State, error)
	Save(ctx context.Context, s *State) error
}

// ModelsConfigRepository loads the parsed models.yaml (RFC.md#configuration).
type ModelsConfigRepository interface {
	Load(ctx context.Context) (*ModelsConfig, error)
}

// ConnectivityChecker answers "do we have internet right now" — required
// before EXECUTE_AGENT dispatch, OPEN_BROWSER for remote URLs, and
// `naira models download` (RFC.md#apis, #configuration).
type ConnectivityChecker interface {
	Online(ctx context.Context) bool
}

// Downloader fetches a URL to a local path and verifies its checksum before
// the caller activates it (RFC.md#configuration, Security Implications).
type Downloader interface {
	// Download fetches url to destPath. If wantSHA256 is non-empty, the
	// downloaded bytes must be verified against it; on mismatch the file
	// must not be left at destPath.
	Download(ctx context.Context, url, destPath, wantSHA256 string) error
}

// AuthChecker reports whether the Claude CLI / OpenCode agent has a valid
// key/authorization present (RFC.md PRD §4.2 action-type gating).
type AuthChecker interface {
	Authorized(ctx context.Context) bool
}

// STTEngine transcribes an in-memory audio buffer (a finalized WAV blob —
// see AudioFrameBytes/AudioSampleRate). Implementations must never persist
// the buffer to disk (RFC.md Security Implications).
type STTEngine interface {
	Transcribe(ctx context.Context, pcm []byte) (string, error)
}

// Audio capture constants: 16kHz mono PCM16, 20ms frames. Fixed across the
// capture/VAD/wake-word/endpointing pipeline (RFC.md#sequence) so every
// consumer of a frame agrees on its size without renegotiation.
const (
	AudioSampleRate     = 16000
	AudioFrameMillis    = 20
	AudioBytesPerSample = 2 // PCM16 mono
)

// AudioFrameBytes is the fixed size (in bytes) of one frame produced by
// AudioCapture: AudioSampleRate * AudioFrameMillis/1000 * AudioBytesPerSample.
const AudioFrameBytes = AudioSampleRate * AudioFrameMillis / 1000 * AudioBytesPerSample

// AudioCapture streams fixed-size raw PCM16 mono frames from the microphone
// continuously. The mic is always open (RFC.md Security Implications: no
// recording, in-memory only) — gating on the wake word happens downstream,
// not at capture time.
type AudioCapture interface {
	// Frames starts capture and returns a channel of AudioFrameBytes-sized
	// PCM16 buffers. The channel closes when ctx is done or capture ends.
	Frames(ctx context.Context) (<-chan []byte, error)
}

// WakeWordDetector reports whether a single audio frame contains/completes
// the wake phrase trigger. Real implementation is an open decision — see
// RFC.md §5 Concerns (engine unspecified); a stub that never fires lets the
// rest of the pipeline be wired and tested regardless.
type WakeWordDetector interface {
	Detect(frame []byte) bool
}

// VADEngine classifies a single audio frame as speech or silence, used for
// utterance endpointing after wake-word trigger (RFC.md#sequence,
// Performance Requirement: Utterance Endpointing Wait).
type VADEngine interface {
	IsSpeech(frame []byte) bool
}

// LLMEngine runs local inference and streams raw tag-prefixed text.
// Sentences are yielded to onSentence as they complete so TTS can start
// speaking before the full response is generated (RFC.md#sequence).
type LLMEngine interface {
	Infer(ctx context.Context, prompt string, onSentence func(sentence string)) (LLMOutput, error)
}

// TTSEngine synthesizes and plays one sentence of speech.
type TTSEngine interface {
	Speak(ctx context.Context, text string) error
}

// AgentJob describes one EXECUTE_AGENT dispatch.
type AgentJob struct {
	Name       string // sanitized app/game name, also the /games/<name>/ dir
	PromptText string
}

// AgentResult is what the sandboxed subprocess produced.
type AgentResult struct {
	IndexHTMLPath string
	Failed        bool
	FailureReason string
}

// AgentEngine spawns the Claude CLI / OpenCode subprocess, sandboxed to
// /games/<name>/, dependency-fetch-only network policy (RFC.md#apis).
type AgentEngine interface {
	Execute(ctx context.Context, job AgentJob) (AgentResult, error)
}

// UIState mirrors the `state_change` IPC message (RFC.md#apis).
type UIState string

// UIPublisher pushes state to the local UI over IPC (WebSocket/native).
type UIPublisher interface {
	SetState(ctx context.Context, state ExpressionTag) error
	SetWindowMode(ctx context.Context, floating bool, w, h int) error
	MouthAmplitude(ctx context.Context, amplitude float64, tsMillis int64) error
	AgentStatus(ctx context.Context, status, jobID string) error
}
