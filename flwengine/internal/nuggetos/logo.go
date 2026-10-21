package nuggetos

import (
	"bytes"
	"image"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"

	_ "embed"
)



//go:embed logo.svg
var logoSVG []byte

var (
	logoOnce sync.Once
	logoImg  *ebiten.Image
	logoRect image.Rectangle
)

func getLogo() *ebiten.Image {
	logoOnce.Do(renderLogo)
	return logoImg
}

func renderLogo() {
	icon, err := oksvg.ReadIconStream(bytes.NewReader(logoSVG))
	if err != nil {
		logoImg = nil
		return
	}
	w, h := icon.ViewBox.W, icon.ViewBox.H
	scale := 160 / w
	tw := int(w * scale)
	th := int(h * scale)
	img := image.NewRGBA(image.Rect(0, 0, tw, th))
	scanner := rasterx.NewScannerGV(tw, th, img, img.Bounds())
	raster := rasterx.NewDasher(tw, th, scanner)
	icon.SetTarget(0, 0, w*scale, h*scale)
	icon.Draw(raster, 1.0)
	logoRect = img.Bounds()
	logoImg = ebiten.NewImageFromImage(img)
}

func logoSize() (int, int) {
	if getLogo() == nil {
		return 0, 0
	}
	return logoRect.Dx(), logoRect.Dy()
}
