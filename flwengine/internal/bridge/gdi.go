package bridge

import (
	"encoding/json"
)

type GDIBackend func(name string, params map[string]any) (any, error)

var gdiBackend GDIBackend

func SetGDIBackend(f GDIBackend) {
	gdiBackend = f
}

func (b *Bridge) handleGDI(args json.RawMessage) Result {
	var p struct {
		Name   string         `json:"name"`
		Params map[string]any `json:"params"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return fail("bad args")
	}
	if gdiBackend == nil {
		return fail("gdi effects not available on this platform")
	}
	if p.Name == "" {
		return fail("empty effect name")
	}
	res, err2 := gdiBackend(p.Name, p.Params)
	if err2 != nil {
		return fail(err2.Error())
	}
	return ok(res)
}
