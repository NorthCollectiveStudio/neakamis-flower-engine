package nuggetos

import (
	"bytes"
	"embed"
	"encoding/binary"
	"fmt"
	"math"
	"sync"
)

const (
	sampleRate     = 44100
	sampleChannels = 2
)

//go:embed assets/meego/sounds
var meegoSoundFS embed.FS

type soundKind int

const (
	sndMove soundKind = iota
	sndSelect
	sndBack
	sndBoot
	sndError
	sndToggle
	sndType
	sndNotif
	sndWarnLow
)

// audioBackend plays the sounds of the engine.
//
// The buffer is always raw 16 bit little endian PCM with two channels and
// the sample rate of sampleRate - that is the format ebiten and PulseAudio
// expect. A wav file with a header must not be handed over: ebiten would
// play the header as samples and read the mono data of a wav file as
// stereo, the sound would be an octave too high and full of clicks.
type audioBackend interface {
	// play sends the buffer to the speakers. It must not block the caller
	// for the length of the sound.
	play(k soundKind, pcm []byte, volume float64)
	// name is the name of the backend for the log.
	name() string
}

type soundEngine struct {
	mu      sync.Mutex
	backend audioBackend
	data    map[soundKind][]byte
	volume  float64
}

var snd *soundEngine

func initSound(volume float64) {
	snd = &soundEngine{
		data:   map[soundKind][]byte{},
		volume: volume,
	}
	snd.embed(sndBoot, "desktop-login.wav")
	snd.embed(sndError, "warn-double.wav")
	snd.embed(sndWarnLow, "warn-low.wav")
	snd.data[sndMove] = renderTick(1720, 0.035, 0.10)
	snd.data[sndSelect] = renderDuo(1174.7, 1567.98, 0.09, 0.16)
	snd.data[sndBack] = renderDuo(880, 587.33, 0.08, 0.13)
	snd.data[sndToggle] = renderTick(1396.9, 0.045, 0.12)
	snd.data[sndType] = renderTick(2093, 0.025, 0.06)
	snd.data[sndNotif] = renderNotif()
	snd.backend = newAudioBackend()
}

func (s *soundEngine) embed(k soundKind, name string) {
	data, err := meegoSoundFS.ReadFile("assets/meego/sounds/" + name)
	if err != nil {
		return
	}
	pcm, err := decodeWav(data)
	if err != nil {
		return
	}
	s.data[k] = pcm
}

func (s *soundEngine) play(k soundKind) {
	if s == nil || s.volume <= 0 {
		return
	}
	s.mu.Lock()
	pcm, ok := s.data[k]
	backend := s.backend
	vol := s.volume
	s.mu.Unlock()
	if !ok || backend == nil {
		return
	}
	backend.play(k, pcm, vol)
}

// soundBackendName returns the name of the audio backend for the log.
func soundBackendName() string {
	if snd == nil || snd.backend == nil {
		return "none"
	}
	return snd.backend.name()
}

func playSound(k soundKind) {
	if snd != nil {
		snd.play(k)
	}
}

func SetSoundVolume(v float64) {
	if snd != nil {
		snd.mu.Lock()
		snd.volume = v
		snd.mu.Unlock()
	}
}

func decodeWav(data []byte) ([]byte, error) {
	if len(data) < 44 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, fmt.Errorf("not a wav")
	}
	off := 12
	for off+8 <= len(data) {
		id := string(data[off : off+4])
		sz := binary.LittleEndian.Uint32(data[off+4 : off+8])
		if id == "data" {
			end := off + 8 + int(sz)
			if end > len(data) {
				end = len(data)
			}
			return data[off+8 : end], nil
		}
		off += 8 + int(sz) + (int(sz) & 1)
	}
	return nil, fmt.Errorf("no data chunk")
}

// renderTick renders a short click as raw stereo pcm.
func renderTick(freq float64, dur float64, vol float64) []byte {
	n := int(dur * sampleRate)
	buf := new(bytes.Buffer)
	buf.Grow(n * sampleChannels * 2)
	for i := 0; i < n; i++ {
		t := float64(i) / sampleRate
		env := math.Exp(-t*26/dur) * (1 - math.Exp(-t*2400))
		v := math.Sin(2*math.Pi*freq*t)*0.7 + math.Sin(2*math.Pi*freq*2.76*t)*0.3
		s := v * env * vol
		writeSample(buf, s)
	}
	return buf.Bytes()
}

// renderDuo renders two tones as raw stereo pcm.
func renderDuo(f1, f2, dur, vol float64) []byte {
	n := int(dur * sampleRate)
	buf := new(bytes.Buffer)
	buf.Grow(n * sampleChannels * 2)
	for i := 0; i < n; i++ {
		t := float64(i) / sampleRate
		env := math.Exp(-t*11/dur) * (1 - math.Exp(-t*1600))
		var v float64
		if t < dur*0.3 {
			v = math.Sin(2*math.Pi*f1*t) * 0.6
		} else {
			v = math.Sin(2*math.Pi*f1*t)*0.25 + math.Sin(2*math.Pi*f2*t)*0.6
		}
		v += math.Sin(2*math.Pi*f2*t) * 0.15
		s := v * env * vol
		writeSample(buf, s)
	}
	return buf.Bytes()
}

// renderNotif renders the notification sound as raw stereo pcm.
func renderNotif() []byte {
	dur := 0.16
	n := int(dur * sampleRate)
	buf := new(bytes.Buffer)
	buf.Grow(n * sampleChannels * 2)
	for i := 0; i < n; i++ {
		t := float64(i) / sampleRate
		env := math.Exp(-t*18) * (1 - math.Exp(-t*900))
		v := math.Sin(2*math.Pi*987.77*t)*0.5 + math.Sin(2*math.Pi*1318.5*t)*0.5
		s := v * env * 0.22
		writeSample(buf, s)
	}
	return buf.Bytes()
}

// writeSample writes one sample to both channels.
func writeSample(buf *bytes.Buffer, v float64) {
	if v > 1 {
		v = 1
	}
	if v < -1 {
		v = -1
	}
	s := int16(v * 32767)
	binary.Write(buf, binary.LittleEndian, s)
	binary.Write(buf, binary.LittleEndian, s)
}

// scalePCM multiplies the samples with the volume. It is used by the
// backends that cannot set the volume of the stream themselves.
func scalePCM(pcm []byte, volume float64) []byte {
	if volume >= 1 {
		return pcm
	}
	out := make([]byte, len(pcm))
	for i := 0; i+1 < len(pcm); i += 2 {
		s := int16(binary.LittleEndian.Uint16(pcm[i:]))
		s = int16(float64(s) * volume)
		binary.LittleEndian.PutUint16(out[i:], uint16(s))
	}
	return out
}
