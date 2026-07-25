package domain

// ModelsConfig is the parsed contents of models.yaml — STT/LLM/TTS model
// identifiers, file paths, download URLs, and checksums. Kept separate from
// State: large binary artifacts don't belong in runtime state, and this file
// is meant to be human-editable (RFC.md#configuration).
type ModelsConfig struct {
	STT      ModelEntry `yaml:"stt" mapstructure:"stt"`
	LLM      ModelEntry `yaml:"llm" mapstructure:"llm"`
	TTS      ModelEntry `yaml:"tts" mapstructure:"tts"`
	WakeWord ModelEntry `yaml:"wakeword" mapstructure:"wakeword"`
}

// ModelEntry describes one model artifact. ConfigPath is only used by TTS
// (Piper ships a companion .onnx.json); it is empty for STT/LLM.
// ServerBin/Port/Args are used by STT/LLM/WakeWord, which run as standalone
// server subprocesses supervised by the orchestrator (whisper-server,
// llama-server, and scripts/openwakeword_server.py respectively) rather than
// CGo bindings (RFC.md#architecture--tech-stack, decision note).
type ModelEntry struct {
	Engine     string   `yaml:"engine" mapstructure:"engine"`
	Model      string   `yaml:"model,omitempty" mapstructure:"model"`
	Voice      string   `yaml:"voice,omitempty" mapstructure:"voice"`
	Quant      string   `yaml:"quant,omitempty" mapstructure:"quant"`
	Path       string   `yaml:"path" mapstructure:"path"`
	ConfigPath string   `yaml:"config_path,omitempty" mapstructure:"config_path"`
	URL        string   `yaml:"url,omitempty" mapstructure:"url"`
	SHA256     string   `yaml:"sha256,omitempty" mapstructure:"sha256"`
	ServerBin  string   `yaml:"server_bin,omitempty" mapstructure:"server_bin"`
	Port       int      `yaml:"port,omitempty" mapstructure:"port"`
	Args       []string `yaml:"args,omitempty" mapstructure:"args"`
}

// HasServer reports whether this entry is configured to run as a supervised
// subprocess (STT/LLM only — see ServerBin doc above).
func (e ModelEntry) HasServer() bool {
	return e.ServerBin != ""
}

// Name identifies the entry for CLI output (naira models list).
func (e ModelEntry) Name() string {
	if e.Voice != "" {
		return e.Voice
	}
	return e.Model
}

// Fetchable reports whether `naira models download` can auto-fetch this
// entry — both URL and checksum must be present (RFC.md#configuration).
func (e ModelEntry) Fetchable() bool {
	return e.URL != "" && e.SHA256 != ""
}

// Subsystem is one of "stt", "llm", "tts" — used to select --only targets.
type Subsystem string

const (
	SubsystemSTT      Subsystem = "stt"
	SubsystemLLM      Subsystem = "llm"
	SubsystemTTS      Subsystem = "tts"
	SubsystemWakeWord Subsystem = "wakeword"
)

// Entries returns all subsystem entries paired with their identifier, in a
// stable order, for iteration by usecases and CLI output.
func (c ModelsConfig) Entries() []struct {
	Subsystem Subsystem
	Entry     ModelEntry
} {
	return []struct {
		Subsystem Subsystem
		Entry     ModelEntry
	}{
		{SubsystemSTT, c.STT},
		{SubsystemLLM, c.LLM},
		{SubsystemTTS, c.TTS},
		{SubsystemWakeWord, c.WakeWord},
	}
}
