//go:build linux && !android

package nuggetos

/*
#define _GNU_SOURCE
#include <dlfcn.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

// The simple API of PulseAudio is loaded at runtime: the engine has to
// start on a system without PulseAudio as well, and then the ALSA path of
// ebiten is used.
//
// Why not the ALSA path alone: on a desktop where the sound is served by
// PulseAudio or PipeWire the ALSA device "default" is often a null device.
// The context opens without an error and the samples are silently thrown
// away - the engine has no sound at all, while the game (WebAudio) has.

typedef void *flw_pa_simple;

typedef flw_pa_simple (*flw_pa_simple_new_fn)(const char *server, const char *name,
	int dir, const char *dev, const char *stream, const void *ss, const void *map,
	const void *attr, char **error);
typedef int (*flw_pa_simple_write_fn)(flw_pa_simple s, const void *data, size_t bytes, char **error);
typedef int (*flw_pa_simple_drain_fn)(flw_pa_simple s, char **error);
typedef void (*flw_pa_free_fn)(void *p);

typedef struct {
	int format;
	uint32_t rate;
	uint8_t channels;
} flw_pa_sample_spec;

static void *flw_dlopen(const char *name) {
	return dlopen(name, RTLD_LAZY | RTLD_LOCAL);
}

static void *flw_dlsym(void *h, const char *name) {
	return dlsym(h, name);
}

// The function pointers are called through these helpers: a go value of a
// C function pointer type cannot be called directly.

static flw_pa_simple flw_call_new(flw_pa_simple_new_fn fn, const char *server,
	const char *name, int dir, const char *dev, const char *stream, const void *ss,
	const void *map, const void *attr, char **error) {
	return fn(server, name, dir, dev, stream, ss, map, attr, error);
}

static int flw_call_write(flw_pa_simple_write_fn fn, flw_pa_simple s, const void *data,
	size_t bytes, char **error) {
	return fn(s, data, bytes, error);
}

static int flw_call_drain(flw_pa_simple_drain_fn fn, flw_pa_simple s, char **error) {
	return fn(s, error);
}

static void flw_call_free(flw_pa_free_fn fn, void *p) {
	if (fn != NULL) fn(p);
}
*/
import "C"

import (
	"fmt"
	"os"
	"sync"
	"unsafe"
)

const (
	paSampleS16LE      = 3
	paStreamPlayback   = 1
	pulseLibName       = "libpulse-simple.so.0"
	pulseFallbackLib   = "libpulse-simple.so"
	pulseApplicationNm = "flower engine"
)

// pulseBackend plays the sounds through the PulseAudio server. PipeWire
// provides that server on the modern desktops, so the sounds of the engine
// and the sounds of the game go through the same mixer.
type pulseBackend struct {
	mu     sync.Mutex
	simple C.flw_pa_simple
	newFn  C.flw_pa_simple_new_fn
	write  C.flw_pa_simple_write_fn
	drain  C.flw_pa_simple_drain_fn
	free   C.flw_pa_free_fn
	jobs   chan pulseJob
}

type pulseJob struct {
	pcm    []byte
	volume float64
}

// newAudioBackend returns the backend for the sounds of the engine.
func newAudioBackend() audioBackend {
	b := &pulseBackend{jobs: make(chan pulseJob, 16)}
	if err := b.open(); err != nil {
		// No PulseAudio (or no server): the ALSA path of ebiten is used.
		fmt.Fprintf(os.Stderr, "audio: pulseaudio is not available (%v), using the alsa path\n", err)
		return newEbitenBackend()
	}
	return b
}

func (b *pulseBackend) name() string {
	return "pulse"
}

// open loads the library and opens the playback stream.
func (b *pulseBackend) open() error {
	handle := C.flw_dlopen(cString(pulseLibName))
	if handle == nil {
		handle = C.flw_dlopen(cString(pulseFallbackLib))
	}
	if handle == nil {
		return fmt.Errorf("%s is not available", pulseLibName)
	}
	b.newFn = C.flw_pa_simple_new_fn(C.flw_dlsym(handle, cString("pa_simple_new")))
	b.write = C.flw_pa_simple_write_fn(C.flw_dlsym(handle, cString("pa_simple_write")))
	b.drain = C.flw_pa_simple_drain_fn(C.flw_dlsym(handle, cString("pa_simple_drain")))
	b.free = C.flw_pa_free_fn(C.flw_dlsym(handle, cString("pa_free")))
	if b.newFn == nil || b.write == nil || b.drain == nil {
		return fmt.Errorf("the simple api of pulseaudio is not complete")
	}
	var spec C.flw_pa_sample_spec
	spec.format = paSampleS16LE
	spec.rate = sampleRate
	spec.channels = sampleChannels
	name := cString(pulseApplicationNm)
	stream := cString("ui sounds")
	defer C.free(unsafe.Pointer(name))
	defer C.free(unsafe.Pointer(stream))
	var cerr *C.char
	simple := C.flw_call_new(b.newFn,
		nil,
		name,
		paStreamPlayback,
		nil,
		stream,
		unsafe.Pointer(&spec),
		nil,
		nil,
		&cerr)
	if simple == nil {
		msg := "no pulseaudio server"
		if cerr != nil {
			msg = C.GoString(cerr)
			b.freeErr(cerr)
		}
		return fmt.Errorf("%s", msg)
	}
	b.simple = simple
	go b.loop()
	return nil
}

func (b *pulseBackend) freeErr(p *C.char) {
	if b.free != nil {
		C.flw_call_free(b.free, unsafe.Pointer(p))
		return
	}
	C.free(unsafe.Pointer(p))
}

func (b *pulseBackend) play(k soundKind, pcm []byte, volume float64) {
	select {
	case b.jobs <- pulseJob{pcm: pcm, volume: volume}:
	default:
		// The queue is full: the sound is dropped instead of blocking the
		// user interface.
	}
}

// loop writes the sounds to the stream. It runs in its own goroutine
// because the write and the drain block until the sound was played.
func (b *pulseBackend) loop() {
	for job := range b.jobs {
		data := scalePCM(job.pcm, job.volume)
		if len(data) == 0 {
			continue
		}
		var cerr *C.char
		n := C.flw_call_write(b.write, b.simple, unsafe.Pointer(&data[0]), C.size_t(len(data)), &cerr)
		if n < 0 {
			if cerr != nil {
				b.freeErr(cerr)
			}
			continue
		}
		C.flw_call_drain(b.drain, b.simple, &cerr)
		if cerr != nil {
			b.freeErr(cerr)
		}
	}
}

// cString converts a go string into a C string that must be freed by the
// caller.
func cString(s string) *C.char {
	return C.CString(s)
}
