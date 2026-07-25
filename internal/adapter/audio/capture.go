// Package audio implements domain.AudioCapture via a standalone recording
// subprocess (arecord/parec) — no CGo audio library needed, consistent with
// the subprocess-supervision pattern already used for whisper-server/
// llama-server (RFC.md#architecture--tech-stack, Dependencies).
package audio

import (
	"context"
	"fmt"
	"io"
	"os/exec"

	"naira/internal/domain"
)

// MicCapture spawns Bin (typically "arecord") with flags requesting raw
// PCM16 mono at domain.AudioSampleRate, and slices its stdout into fixed
// domain.AudioFrameBytes-sized frames.
type MicCapture struct {
	Bin  string   // e.g. "arecord"
	Args []string // extra args (e.g. -D <device>) appended before the format flags
}

func NewMicCapture(bin string, args []string) *MicCapture {
	return &MicCapture{Bin: bin, Args: args}
}

func (m *MicCapture) Frames(ctx context.Context) (<-chan []byte, error) {
	args := append(append([]string{}, m.Args...),
		"-q", "-t", "raw",
		"-f", "S16_LE",
		"-c", "1",
		"-r", fmt.Sprintf("%d", domain.AudioSampleRate),
	)

	cmd := exec.CommandContext(ctx, m.Bin, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("pipe %s stdout: %w", m.Bin, err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("spawn %s: %w", m.Bin, err)
	}

	ch := make(chan []byte, 8)
	go func() {
		defer close(ch)
		defer cmd.Wait()
		buf := make([]byte, domain.AudioFrameBytes)
		for {
			if _, err := io.ReadFull(stdout, buf); err != nil {
				return // capture ended (ctx cancelled, subprocess exited, or EOF)
			}
			frame := make([]byte, domain.AudioFrameBytes)
			copy(frame, buf)
			select {
			case ch <- frame:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}
