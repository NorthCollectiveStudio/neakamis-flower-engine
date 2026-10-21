package nuggetos

import (
	"encoding/binary"
	"testing"
)

// The sounds are handed over to the audio backend as raw pcm: 16 bit little
// endian, two channels, no wav header. A wav file with a header made ebiten
// play the header as samples and read the mono data as stereo - the sound
// was an octave too high and full of clicks.
func TestRenderedSoundsAreRawStereoPCM(t *testing.T) {
	cases := map[string][]byte{
		"move":   renderTick(1720, 0.035, 0.10),
		"select": renderDuo(1174.7, 1567.98, 0.09, 0.16),
		"back":   renderDuo(880, 587.33, 0.08, 0.13),
		"toggle": renderTick(1396.9, 0.045, 0.12),
		"type":   renderTick(2093, 0.025, 0.06),
		"notif":  renderNotif(),
	}
	for name, pcm := range cases {
		if len(pcm) == 0 {
			t.Fatalf("%s: empty", name)
		}
		if len(pcm)%(sampleChannels*2) != 0 {
			t.Errorf("%s: %d bytes is not a whole number of stereo samples", name, len(pcm))
		}
		if string(pcm[:4]) == "RIFF" {
			t.Errorf("%s: the buffer still has a wav header", name)
		}
		// The two channels of a sample must be the same: the sound is mono,
		// the hardware wants stereo.
		for i := 0; i+3 < len(pcm); i += 4 {
			l := binary.LittleEndian.Uint16(pcm[i:])
			r := binary.LittleEndian.Uint16(pcm[i+2:])
			if l != r {
				t.Errorf("%s: the channels differ at %d (%d != %d)", name, i, l, r)
				break
			}
		}
	}
}

// The wav files of the engine are real wav files: they are decoded to pcm
// before they are played.
func TestEmbeddedSoundsAreDecoded(t *testing.T) {
	for _, name := range []string{"desktop-login.wav", "warn-double.wav", "warn-low.wav"} {
		raw, err := meegoSoundFS.ReadFile("assets/meego/sounds/" + name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		pcm, err := decodeWav(raw)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(pcm) == 0 {
			t.Errorf("%s: no samples", name)
		}
		if len(pcm)%(sampleChannels*2) != 0 {
			t.Errorf("%s: %d bytes is not a whole number of stereo samples", name, len(pcm))
		}
	}
}

func TestScalePCM(t *testing.T) {
	pcm := []byte{0x10, 0x00, 0x20, 0x00} // 16 and 32
	out := scalePCM(pcm, 0.5)
	if got := binary.LittleEndian.Uint16(out[0:]); got != 8 {
		t.Errorf("the sample was not scaled: %d", got)
	}
	if got := binary.LittleEndian.Uint16(out[2:]); got != 16 {
		t.Errorf("the sample was not scaled: %d", got)
	}
	// A volume of 1 returns the buffer unchanged.
	if got := scalePCM(pcm, 1.0); &got[0] != &pcm[0] {
		t.Error("the buffer was copied although the volume is 1")
	}
}
