package gdi

import (
	"errors"
	"strings"
	"sync"
)

func errorsNew(s string) error { return errors.New(s) }

type job struct {
	name   string
	params map[string]any
}

type animState struct {
	r, g, b    uint8
	curA      uint8
	targetA   uint8
	rampPerMs float64
	hold      bool
	flashing  bool
	flashPeak uint8
	flashUp   float64
	flashDown float64
	flashT    float64
	active    bool
}

type Manager struct {
	jobs    chan job
	acks    chan error
	quit    chan struct{}
	stopped sync.WaitGroup
	wine    bool
	live    bool
	mu      sync.Mutex
}

func NewManager(wine bool) *Manager {
	return &Manager{
		jobs: make(chan job, 32),
		acks: make(chan error, 32),
		quit: make(chan struct{}),
		wine: wine,
	}
}

func (m *Manager) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.live
}

func paramColor(p map[string]any, key string, def string) (uint8, uint8, uint8) {
	v, ok := p[key]
	if !ok {
		v, ok = p["color"]
	}
	if !ok {
		return hexToRGB(def)
	}
	switch t := v.(type) {
	case string:
		return hexToRGB(t)
	case float64:
		n := uint32(t) & 0xFFFFFF
		return uint8((n >> 16) & 0xFF), uint8((n >> 8) & 0xFF), uint8(n & 0xFF)
	}
	return hexToRGB(def)
}

func paramInt(p map[string]any, key string, def float64) float64 {
	if v, ok := p[key]; ok {
		if f, ok2 := v.(float64); ok2 {
			return f
		}
	}
	return def
}

func hexToRGB(s string) (uint8, uint8, uint8) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) != 6 {
		return 255, 255, 255
	}
	var v [3]uint8
	for i := 0; i < 3; i++ {
		var n int
		for j := 0; j < 2; j++ {
			c := s[i*2+j] | 0x20
			var d int
			if c >= '0' && c <= '9' {
				d = int(c - '0')
			} else if c >= 'a' && c <= 'f' {
				d = int(c-'a') + 10
			} else {
				return 255, 255, 255
			}
			n = n*16 + d
		}
		v[i] = uint8(n)
	}
	return v[0], v[1], v[2]
}

func clampA(a float64) uint8 {
	if a < 0 {
		return 0
	}
	if a > 255 {
		return 255
	}
	return uint8(a)
}

func (m *Manager) Handle(name string, params map[string]any) (any, error) {
	m.mu.Lock()
	wine := m.wine
	live := m.live
	m.mu.Unlock()
	if wine {
		return nil, errWine
	}
	if !live {
		return nil, errNoWindow
	}
	m.jobs <- job{name: name, params: params}
	select {
	case e := <-m.acks:
		if e != nil {
			return nil, e
		}
		return map[string]any{"effect": name}, nil
	default:
		return map[string]any{"effect": name, "queued": true}, nil
	}
}

func (m *Manager) applyJob(a *animState, j job) error {
	switch j.name {
	case "flash":
		r, g, b := paramColor(j.params, "color", "#ffffff")
		a.r, a.g, a.b = r, g, b
		a.flashing = true
		a.flashPeak = clampA(paramInt(j.params, "alpha", 220))
		a.flashUp = paramInt(j.params, "up", 60)
		a.flashDown = paramInt(j.params, "down", 420)
		if a.flashUp < 1 {
			a.flashUp = 1
		}
		if a.flashDown < 1 {
			a.flashDown = 1
		}
		a.flashT = 0
		a.active = true
		a.curA = 0
	case "overlay":
		r, g, b := paramColor(j.params, "color", "#000000")
		a.r, a.g, a.b = r, g, b
		a.flashing = false
		a.targetA = clampA(paramInt(j.params, "alpha", 160))
		ramp := paramInt(j.params, "ms", 200)
		if ramp < 1 {
			ramp = 1
		}
		a.rampPerMs = float64(a.targetA-a.curA) / ramp
		a.hold = true
		a.active = true
	case "fadeout":
		r, g, b := paramColor(j.params, "color", "#000000")
		a.r, a.g, a.b = r, g, b
		a.flashing = false
		a.targetA = clampA(paramInt(j.params, "alpha", 255))
		ramp := paramInt(j.params, "ms", 800)
		if ramp < 1 {
			ramp = 1
		}
		a.rampPerMs = float64(a.targetA-a.curA) / ramp
		a.hold = true
		a.active = true
	case "fadein":
		a.flashing = false
		a.targetA = 0
		ramp := paramInt(j.params, "ms", 800)
		if ramp < 1 {
			ramp = 1
		}
		a.rampPerMs = float64(0-float64(a.curA)) / ramp
		a.hold = false
		a.active = true
	case "clear":
		a.flashing = false
		a.targetA = 0
		a.rampPerMs = -float64(a.curA) / 60
		a.hold = false
		a.active = true
	default:
		return errUnknownEffect
	}
	return nil
}

func (m *Manager) tick(a *animState, dtMs float64) bool {
	if !a.active {
		return false
	}
	if a.flashing {
		a.flashT += dtMs
		if a.flashT <= a.flashUp {
			a.curA = uint8(float64(a.flashPeak) * (a.flashT / a.flashUp))
		} else if a.flashT <= a.flashUp+a.flashDown {
			a.curA = uint8(float64(a.flashPeak) * (1 - (a.flashT-a.flashUp)/a.flashDown))
		} else {
			a.active = false
			a.curA = 0
		}
		return true
	}
	if a.curA == a.targetA {
		if a.hold {
			return false
		}
		a.active = false
		return true
	}
	step := a.rampPerMs * dtMs
	cur := float64(a.curA)
	next := cur + step
	if (step > 0 && next >= float64(a.targetA)) || (step < 0 && next <= float64(a.targetA)) {
		a.curA = a.targetA
		if !a.hold {
			a.active = false
		}
	} else {
		a.curA = uint8(next)
	}
	return true
}
