package nuggetos

import (
	"bytes"
	"embed"
	"image"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
	_ "image/png"
)



//go:embed assets/meego/cursors
var meegoCursorFS embed.FS


type cursorKind int

const (
	curArrow cursorKind = iota
	curHand
	curText
	curBusy
	curHResize
)

func (c cursorKind) file() string {
	switch c {
	case curHand:
		return "hand2.png"
	case curText:
		return "xterm.png"
	case curBusy:
		return "left_ptr_watch_%04d.png"
	case curHResize:
		return "sb_h_double_arrow.png"
	default:
		return "left_ptr.png"
	}
}


func (c cursorKind) hotspot() (float64, float64) {
	switch c {
	case curHand:
		return 9, 5
	case curText:
		return 11, 11
	case curHResize:
		return 11, 11
	default:
		return 1, 1
	}
}

const (
	cursorScale     = 1.25 
	busyFrameCount  = 25
	busyFrameMillis = 30
)

type cursorStore struct {
	once   sync.Once
	single map[cursorKind]*ebiten.Image
	busy   []*ebiten.Image
}

var cursors cursorStore

func (cs *cursorStore) load() {
	cs.single = map[cursorKind]*ebiten.Image{}
	for _, k := range []cursorKind{curArrow, curHand, curText, curHResize} {
		cs.single[k] = loadCursorPNG(k.file())
	}
	for i := 1; i <= busyFrameCount; i++ {
		name := "left_ptr_watch.png"
		if i > 0 {
			
			name = sprintfFrame(i)
		}
		if img := loadCursorPNG(name); img != nil {
			cs.busy = append(cs.busy, img)
		}
	}
	if len(cs.busy) == 0 {
		cs.busy = append(cs.busy, loadCursorPNG("left_ptr_watch.png"))
	}
}

func sprintfFrame(i int) string {
	b := [4]byte{}
	n := i
	for p := 3; p >= 0; p-- {
		b[p] = byte('0' + n%10)
		n /= 10
	}
	return "left_ptr_watch_" + string(b[:]) + ".png"
}

func loadCursorPNG(name string) *ebiten.Image {
	data, err := meegoCursorFS.ReadFile("assets/meego/cursors/" + name)
	if err != nil {
		return nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	return ebiten.NewImageFromImage(img)
}




var uiCursor cursorKind = curArrow


var forceBusyCursor bool



func drawCursor(screen *ebiten.Image, t float64) {
	mx, my := ebiten.CursorPosition()
	kind := uiCursor
	if forceBusyCursor {
		kind = curBusy
	}
	var img *ebiten.Image
	hx, hy := kind.hotspot()
	if kind == curBusy {
		cursors.once.Do(cursors.load)
		if len(cursors.busy) == 0 {
			return
		}
		idx := int(t*1000/busyFrameMillis) % len(cursors.busy)
		img = cursors.busy[idx]
		hx, hy = 1, 1
	} else {
		cursors.once.Do(cursors.load)
		img = cursors.single[kind]
	}
	if img == nil {
		return
	}
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(cursorScale, cursorScale)
	op.GeoM.Translate(float64(mx)-hx*cursorScale, float64(my)-hy*cursorScale)
	screen.DrawImage(img, op)
}
