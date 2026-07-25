// Package sound implements domain.SoundBoard: short pre-recorded audio cues
// (greeting/ack/thinking hum), embedded into the binary and played by
// piping WAV bytes into a playback subprocess (aplay by default) — same
// no-CGo subprocess posture as internal/adapter/tts.PiperCLI. Distinct from
// TTS: these are fixed clips, not synthesized speech.
package sound

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"io/fs"
	"math/rand"
	"os/exec"

	"naira/internal/domain"
)

//go:embed static
var clipFiles embed.FS

var categoryDirs = map[domain.SoundCategory]string{
	domain.SoundGreeting: "static/greeting_sounds",
	domain.SoundAck:      "static/ack_sounds",
	domain.SoundThinking: "static/thinking_sounds",
}

// Board implements domain.SoundBoard.
type Board struct {
	PlayerBin  string
	PlayerArgs []string
}

func NewBoard(playerBin string, playerArgs []string) *Board {
	return &Board{PlayerBin: playerBin, PlayerArgs: playerArgs}
}

// Play picks a random clip from category and plays it, blocking until
// playback finishes. Never returns an error for "no clips in this
// category" — that's a valid, silent no-op (lets a customized character
// with fewer clip folders work without special-casing).
func (b *Board) Play(ctx context.Context, category domain.SoundCategory) error {
	dir, ok := categoryDirs[category]
	if !ok {
		return fmt.Errorf("sound: unknown category %q", category)
	}

	entries, err := fs.ReadDir(clipFiles, dir)
	if err != nil || len(entries) == 0 {
		return nil
	}
	clip := entries[rand.Intn(len(entries))]

	data, err := fs.ReadFile(clipFiles, dir+"/"+clip.Name())
	if err != nil {
		return fmt.Errorf("sound: read embedded clip %s: %w", clip.Name(), err)
	}

	args := append(append([]string{}, b.PlayerArgs...), "-q", "-")
	cmd := exec.CommandContext(ctx, b.PlayerBin, args...)
	cmd.Stdin = bytes.NewReader(data)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sound: play %s: %w", clip.Name(), err)
	}
	return nil
}
