// Package listening implements the wake-word-gated, VAD-endpointed capture
// loop: continuously classify incoming frames, seed each utterance with a
// pre-roll buffer once the wake word fires, cut on sustained silence (or a
// max-length safety cap), then hand the finalized WAV blob to STT
// (RFC.md#sequence Core Conversation Flow, Performance Requirement).
package listening

import (
	"context"
	"time"

	"naira/internal/domain"
)

const frameDuration = domain.AudioFrameMillis * time.Millisecond

// Options tunes the endpointing state machine. Defaults match the starting
// values documented in RFC.md §5 Concerns (unvalidated, tune in Phase 1).
type Options struct {
	SilenceTimeout time.Duration // cut after this much continuous non-speech
	PreRoll        time.Duration // lookback seeded into the utterance at wake trigger
	MaxUtterance   time.Duration // hard cap regardless of VAD (safety against stuck LISTENING)
}

func DefaultOptions() Options {
	return Options{
		SilenceTimeout: 700 * time.Millisecond,
		PreRoll:        300 * time.Millisecond,
		MaxUtterance:   20 * time.Second,
	}
}

// Service wires AudioCapture -> WakeWordDetector -> VADEngine -> STTEngine
// into the endpointing state machine.
type Service struct {
	Capture domain.AudioCapture
	Wake    domain.WakeWordDetector
	VAD     domain.VADEngine
	STT     domain.STTEngine
	Opts    Options
}

func New(capture domain.AudioCapture, wake domain.WakeWordDetector, vadEngine domain.VADEngine, stt domain.STTEngine, opts Options) *Service {
	return &Service{Capture: capture, Wake: wake, VAD: vadEngine, STT: stt, Opts: opts}
}

type state int

const (
	stateIdle state = iota
	stateListening
)

// Run blocks, consuming frames until ctx is done or capture ends. onTranscript
// is invoked once per finalized, non-empty utterance.
func (s *Service) Run(ctx context.Context, onTranscript func(ctx context.Context, transcript string)) error {
	frames, err := s.Capture.Frames(ctx)
	if err != nil {
		return err
	}

	preRollFrames := int(s.Opts.PreRoll / frameDuration)
	if preRollFrames < 0 {
		preRollFrames = 0
	}
	ring := make([][]byte, 0, preRollFrames)

	st := stateIdle
	var utterance [][]byte
	var silence time.Duration
	var elapsed time.Duration

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case frame, ok := <-frames:
			if !ok {
				return nil
			}

			switch st {
			case stateIdle:
				ring = append(ring, frame)
				if len(ring) > preRollFrames {
					ring = ring[len(ring)-preRollFrames:]
				}

				if s.Wake.Detect(frame) {
					st = stateListening
					utterance = append([][]byte(nil), ring...)
					silence = 0
					elapsed = 0
				}

			case stateListening:
				utterance = append(utterance, frame)
				elapsed += frameDuration

				if s.VAD.IsSpeech(frame) {
					silence = 0
				} else {
					silence += frameDuration
				}

				if silence >= s.Opts.SilenceTimeout || elapsed >= s.Opts.MaxUtterance {
					s.finalize(ctx, utterance, onTranscript)
					st = stateIdle
					utterance = nil
					ring = ring[:0]
				}
			}
		}
	}
}

func (s *Service) finalize(ctx context.Context, utterance [][]byte, onTranscript func(context.Context, string)) {
	if len(utterance) == 0 {
		return
	}

	pcm := make([]byte, 0, len(utterance)*domain.AudioFrameBytes)
	for _, f := range utterance {
		pcm = append(pcm, f...)
	}
	wav := domain.EncodeWAV(pcm, domain.AudioSampleRate)

	text, err := s.STT.Transcribe(ctx, wav)
	if err != nil || text == "" {
		return
	}
	onTranscript(ctx, text)
}
