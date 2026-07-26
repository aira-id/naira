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
	"strconv"

	"naira/internal/domain"
)

// MicCapture spawns Bin (typically "arecord") with flags requesting raw
// PCM16 mono, and slices its stdout into fixed domain.AudioFrameBytes-sized
// frames at domain.AudioSampleRate.
type MicCapture struct {
	Bin  string   // e.g. "arecord"
	Args []string // extra args (e.g. -D <device>) appended before the format flags

	// CaptureRate is the rate requested from the recording subprocess. Zero
	// (the default) means domain.AudioSampleRate — no resampling, and
	// byte-identical behavior to before this field existed. Set it when the
	// mic hardware doesn't cleanly support 16kHz (some devices/ALSA
	// configs refuse or glitch on non-native rates even via the "plughw"
	// software-resampling layer); Frames then resamples in Go instead
	// (see Resample), a manual fallback since real-time device-capability
	// probing isn't something validated here without the target hardware
	// in hand (RFC.md §5 Concerns).
	CaptureRate int
}

func NewMicCapture(bin string, args []string) *MicCapture {
	return &MicCapture{Bin: bin, Args: args}
}

func (m *MicCapture) Frames(ctx context.Context) (<-chan []byte, error) {
	captureRate := m.CaptureRate
	if captureRate == 0 {
		captureRate = domain.AudioSampleRate
	}

	args := append(append([]string{}, m.Args...),
		"-q", "-t", "raw",
		"-f", "S16_LE",
		"-c", "1",
		"-r", strconv.Itoa(captureRate),
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
	if captureRate == domain.AudioSampleRate {
		go m.emitDirect(ctx, stdout, cmd, ch)
	} else {
		go m.emitResampled(ctx, stdout, cmd, ch, captureRate)
	}
	return ch, nil
}

// emitDirect is the original, resample-free path: read fixed-size frames
// straight off the subprocess and forward them unchanged.
func (m *MicCapture) emitDirect(ctx context.Context, stdout io.Reader, cmd *exec.Cmd, ch chan<- []byte) {
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
}

// emitResampled reads captureRate-sized chunks, resamples each to
// domain.AudioSampleRate, and re-slices the (variable-length, due to
// rounding) resampled bytes into fixed domain.AudioFrameBytes frames —
// carrying leftover bytes over to the next chunk so every frame handed
// downstream is exactly the size the rest of the pipeline assumes.
func (m *MicCapture) emitResampled(ctx context.Context, stdout io.Reader, cmd *exec.Cmd, ch chan<- []byte, captureRate int) {
	defer close(ch)
	defer cmd.Wait()

	captureChunkBytes := domain.AudioFrameBytes * captureRate / domain.AudioSampleRate
	captureChunkBytes -= captureChunkBytes % 2 // stay sample-aligned (PCM16 = 2 bytes/sample)
	raw := make([]byte, captureChunkBytes)

	var pending []byte
	for {
		if _, err := io.ReadFull(stdout, raw); err != nil {
			return
		}
		pending = append(pending, Resample(raw, captureRate, domain.AudioSampleRate)...)

		for len(pending) >= domain.AudioFrameBytes {
			frame := make([]byte, domain.AudioFrameBytes)
			copy(frame, pending[:domain.AudioFrameBytes])
			pending = pending[domain.AudioFrameBytes:]
			select {
			case ch <- frame:
			case <-ctx.Done():
				return
			}
		}
	}
}
