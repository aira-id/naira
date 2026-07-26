package audio

import "encoding/binary"

// Resample converts PCM16LE mono pcm from fromRate to toRate via linear
// interpolation — no CGo/DSP dependency, cheap enough for real-time use on
// the target hardware's 2-core budget. Good enough quality for speech at
// the ratios this project needs (mic native rate -> domain.AudioSampleRate);
// not intended for high-fidelity audio resampling in general.
//
// Ported in spirit from be-more-agent's hardware-aware resampling (which
// used nearest-neighbor slicing to avoid a scipy.signal.resample CPU spike
// on a Raspberry Pi) — linear interpolation here instead, since Go has no
// equivalent heavy DSP call to avoid in the first place.
func Resample(pcm []byte, fromRate, toRate int) []byte {
	if fromRate == toRate || len(pcm) < 2 {
		return pcm
	}

	n := len(pcm) / 2
	in := make([]int16, n)
	for i := range n {
		in[i] = int16(binary.LittleEndian.Uint16(pcm[i*2 : i*2+2]))
	}

	outN := n * toRate / fromRate
	out := make([]byte, outN*2)
	ratio := float64(fromRate) / float64(toRate)

	for i := range outN {
		srcPos := float64(i) * ratio
		idx := int(srcPos)
		frac := srcPos - float64(idx)

		var s float64
		switch {
		case idx+1 < n:
			s = float64(in[idx])*(1-frac) + float64(in[idx+1])*frac
		case idx < n:
			s = float64(in[idx])
		}

		binary.LittleEndian.PutUint16(out[i*2:i*2+2], uint16(int16(s)))
	}
	return out
}
