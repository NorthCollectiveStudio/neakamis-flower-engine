package nuggetos

import (
	"bytes"
	_ "embed"
	"sync"

	"github.com/hajimehoshi/ebiten/v2/text/v2"
)




//go:embed assets/meego/fonts/DroidSans.ttf
var fontRegular []byte



//go:embed assets/meego/fonts/DroidSans-Bold.ttf
var fontBold []byte



//go:embed assets/fonts/DejaVuSansMono.ttf
var fontMono []byte

var (
	srcOnce    sync.Once
	srcRegular *text.GoTextFaceSource
	srcSemi    *text.GoTextFaceSource
	srcBold    *text.GoTextFaceSource
	srcMono    *text.GoTextFaceSource
)

func sources() (*text.GoTextFaceSource, *text.GoTextFaceSource, *text.GoTextFaceSource, *text.GoTextFaceSource) {
	srcOnce.Do(func() {
		srcRegular, _ = text.NewGoTextFaceSource(bytes.NewReader(fontRegular))
		srcSemi, _ = text.NewGoTextFaceSource(bytes.NewReader(fontRegular))
		srcBold, _ = text.NewGoTextFaceSource(bytes.NewReader(fontBold))
		srcMono, _ = text.NewGoTextFaceSource(bytes.NewReader(fontMono))
	})
	return srcRegular, srcSemi, srcBold, srcMono
}

type faceKey struct {
	size float64
	mono bool
	bold int
}

var (
	faceCache = map[faceKey]*text.GoTextFace{}
	faceMu    sync.Mutex
)

func face(size float64, mono bool, bold int) *text.GoTextFace {
	faceMu.Lock()
	defer faceMu.Unlock()
	k := faceKey{size: size, mono: mono, bold: bold}
	if f, ok := faceCache[k]; ok {
		return f
	}
	reg, semi, bd, mo := sources()
	var src *text.GoTextFaceSource
	switch {
	case mono:
		src = mo
	case bold == 2:
		src = bd
	case bold == 1:
		src = semi
	default:
		src = reg
	}
	if src == nil {
		src = reg
	}
	f := &text.GoTextFace{Source: src, Size: size}
	faceCache[k] = f
	return f
}

func regular(size float64) *text.GoTextFace  { return face(size, false, 0) }
func medium(size float64) *text.GoTextFace   { return face(size, false, 1) }
func semibold(size float64) *text.GoTextFace { return face(size, false, 2) }
func boldFace(size float64) *text.GoTextFace { return face(size, false, 2) }
func monoFace(size float64) *text.GoTextFace { return face(size, true, 0) }

func measure(s string, f *text.GoTextFace, spacing float64) (float64, float64) {
	w, h := text.Measure(s, f, spacing)
	return w, h
}
