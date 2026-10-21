//go:build flwnodash





package nuggetos

import (
	"github.com/hajimehoshi/ebiten/v2"
)

type dashboard struct {
        iconTile *ebiten.Image
}

func newDashboard() *dashboard {
	return &dashboard{}
}

func (d *dashboard) enter() {}

func (d *dashboard) update(dt float64, a *app) {}

func (d *dashboard) draw(screen *ebiten.Image, a *app) {
	fillRect(screen, 0, 0, LogicalW, LogicalH, ColBgDeep)
}



func (d *dashboard) loadIcon() {}
