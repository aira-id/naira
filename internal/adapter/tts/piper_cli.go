// Package tts implements domain.TTSEngine via a standalone `piper` CLI
// subprocess, spawned once per sentence (Piper ships no HTTP server mode
// like whisper.cpp/llama.cpp, so it can't reuse the supervised-server
// pattern — RFC.md#architecture--tech-stack decision note). No CGo, keeping
// the same no-CGo posture as STT/LLM/wake-word.
package tts

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const defaultSampleRate = 22050

// PiperCLI synthesizes speech by piping text into `piper --output-raw`
// (raw PCM16 mono at the voice's sample rate on stdout) and streaming that
// directly into a playback subprocess (aplay-compatible: -f S16_LE -c 1
// -r <rate> -t raw), mirroring the format-flag convention already used by
// internal/adapter/audio.MicCapture for the capture side.
type PiperCLI struct {
	Bin        string // path to the piper binary
	ModelPath  string
	ConfigPath string   // Piper's companion <voice>.onnx.json (holds sample_rate)
	ExtraArgs  []string // extra flags from models.yaml tts.args, appended after --output-raw
	PlayerBin  string   // e.g. "aplay"
	PlayerArgs []string // extra args (e.g. -D <device>) before the format flags

	sampleRate int
}

// NewPiperCLI reads sampleRate out of configPath (falls back to
// defaultSampleRate if the file is missing or doesn't have the expected
// shape — Piper will still play, just wrong-speed, so this is a soft
// failure rather than a construction error).
func NewPiperCLI(bin, modelPath, configPath string, extraArgs []string, playerBin string, playerArgs []string) *PiperCLI {
	return &PiperCLI{
		Bin:        bin,
		ModelPath:  modelPath,
		ConfigPath: configPath,
		ExtraArgs:  extraArgs,
		PlayerBin:  playerBin,
		PlayerArgs: playerArgs,
		sampleRate: readSampleRate(configPath),
	}
}

func readSampleRate(configPath string) int {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return defaultSampleRate
	}
	var cfg struct {
		Audio struct {
			SampleRate int `json:"sample_rate"`
		} `json:"audio"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil || cfg.Audio.SampleRate == 0 {
		return defaultSampleRate
	}
	return cfg.Audio.SampleRate
}

// Speak synthesizes text and plays it, blocking until playback finishes.
// piper and the player are connected directly by an OS pipe (not buffered
// in Go) so playback starts as soon as piper emits its first samples,
// preserving the sentence-level streaming latency win the LLM->TTS design
// relies on (RFC.md#sequence).
func (p *PiperCLI) Speak(ctx context.Context, text string) error {
	piperArgs := append([]string{"--model", p.ModelPath, "--config", p.ConfigPath, "--output-raw"}, p.ExtraArgs...)
	piperCmd := exec.CommandContext(ctx, p.Bin, piperArgs...)
	piperCmd.Stdin = strings.NewReader(text + "\n")

	piperOut, err := piperCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("pipe piper stdout: %w", err)
	}

	playerArgs := append(append([]string{}, p.PlayerArgs...),
		"-q", "-t", "raw",
		"-f", "S16_LE",
		"-c", "1",
		"-r", strconv.Itoa(p.sampleRate),
	)
	playerCmd := exec.CommandContext(ctx, p.PlayerBin, playerArgs...)
	playerCmd.Stdin = piperOut

	if err := playerCmd.Start(); err != nil {
		return fmt.Errorf("spawn %s: %w", p.PlayerBin, err)
	}
	if err := piperCmd.Start(); err != nil {
		_ = playerCmd.Process.Kill()
		return fmt.Errorf("spawn piper: %w", err)
	}

	piperErr := piperCmd.Wait()
	playerErr := playerCmd.Wait()
	if piperErr != nil {
		return fmt.Errorf("piper: %w", piperErr)
	}
	if playerErr != nil {
		return fmt.Errorf("%s: %w", p.PlayerBin, playerErr)
	}
	return nil
}
