//go:build !linux || android

package nuggetos

// newAudioBackend returns the backend for the sounds of the engine.
//
// On windows and macos the audio context of ebiten (WASAPI / CoreAudio) is
// used, on android the Oboe driver of oto.
func newAudioBackend() audioBackend {
	return newEbitenBackend()
}
