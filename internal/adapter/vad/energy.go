// Package vad implements domain.VADEngine. Energy is a pure-Go RMS-threshold
// classifier with an adaptive noise floor — no CGo dependency. Simpler and
// cheaper than a spectral classifier (e.g. WebRTC VAD) but more sensitive to
// background noise; documented as an upgrade path in RFC.md §5 Concerns.
package vad

import (
	"encoding/binary"
	"math"
)

// Energy classifies a frame as speech if its RMS exceeds the adaptively
// tracked noise floor by Margin. The noise floor drifts toward the current
// RMS (exponential moving average) only on frames classified as silence, so
// a sustained loud sound doesn't drag the floor up and desensitize the
// detector mid-utterance.
type Energy struct {
	// Margin is added to the noise floor to decide the speech threshold.
	// Tune empirically (RFC.md §5 Concerns: endpointing timing unvalidated).
	Margin float64
	// Alpha is the noise-floor adaptation rate (0-1, higher = faster).
	Alpha float64

	noiseFloor float64
}

func NewEnergy() *Energy {
	return &Energy{Margin: 400, Alpha: 0.05}
}

func (e *Energy) IsSpeech(frame []byte) bool {
	rms := rms16(frame)
	speech := rms > e.noiseFloor+e.Margin
	if !speech {
		e.noiseFloor = e.noiseFloor*(1-e.Alpha) + rms*e.Alpha
	}
	return speech
}

func rms16(frame []byte) float64 {
	n := len(frame) / 2
	if n == 0 {
		return 0
	}
	var sumSquares float64
	for i := 0; i < n; i++ {
		s := int16(binary.LittleEndian.Uint16(frame[i*2 : i*2+2]))
		sumSquares += float64(s) * float64(s)
	}
	return math.Sqrt(sumSquares / float64(n))
}
