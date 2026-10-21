package nuggetos

import (
	"sync"

	"github.com/hajimehoshi/ebiten/v2/audio"
)

// ebitenBackend plays the sounds through the audio context of ebiten.
//
// On windows that is WASAPI, on linux ALSA. The context is created with the
// first sound: on linux the device of ALSA is often a null device when the
// sound is served by PulseAudio, the pulse backend is used there instead.
type ebitenBackend struct {
	mu      sync.Mutex
	ctx     *audio.Context
	players map[soundKind]*audio.Player
}

func newEbitenBackend() audioBackend {
	return &ebitenBackend{players: map[soundKind]*audio.Player{}}
}

func (b *ebitenBackend) name() string {
	return "ebiten"
}

func (b *ebitenBackend) play(k soundKind, pcm []byte, volume float64) {
	b.mu.Lock()
	if b.ctx == nil {
		// The context must exist only once per process.
		b.ctx = audio.NewContext(sampleRate)
	}
	p := b.players[k]
	if p == nil {
		p = b.ctx.NewPlayerFromBytes(pcm)
		if p == nil {
			b.mu.Unlock()
			return
		}
		b.players[k] = p
	}
	b.mu.Unlock()
	p.SetVolume(volume)
	p.Rewind()
	p.Play()
}
