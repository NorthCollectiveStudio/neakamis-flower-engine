package supervisor

import "testing"

// The stderr of the game host is the only place where the reason of a
// failure shows up (a missing WebView2 runtime on windows for example).
// The tail has to find that line in the noise of the web view.
func TestStderrTailFindsTheError(t *testing.T) {
	tail := newStderrTail(8192)
	tail.echo = false
	tail.Write([]byte("XGB: conn.go:48: Trying connection without authority info...\n"))
	tail.Write([]byte("MESA-EGL: warning: DRI3 error: Could not get DRI3 device\n"))
	tail.Write([]byte("the game window failed: the web view could not be created\n"))
	got := tail.lastError()
	want := "the game window failed: the web view could not be created"
	if got != want {
		t.Errorf("lastError() = %q, want %q", got, want)
	}
}

func TestStderrTailIgnoresTheNoise(t *testing.T) {
	tail := newStderrTail(8192)
	tail.echo = false
	tail.Write([]byte("XGB: conn.go:48: noise\n"))
	tail.Write([]byte("MESA-EGL: warning: DRI3 error\n"))
	if got := tail.lastError(); got != "" {
		t.Errorf("lastError() = %q, want an empty string", got)
	}
}

// The tail is bounded: the output of the game host must not grow without a
// limit.
func TestStderrTailIsBounded(t *testing.T) {
	tail := newStderrTail(64)
	tail.echo = false
	for i := 0; i < 100; i++ {
		tail.Write([]byte("0123456789"))
	}
	tail.mu.Lock()
	n := len(tail.buf)
	tail.mu.Unlock()
	if n > 64 {
		t.Errorf("the tail has %d bytes, the limit is 64", n)
	}
}
