package app

import (
	_ "embed"

	"github.com/egoist/mygo/ui"
)

//go:embed assets/logo.png
var logoPNG []byte

var logoImage = func() ui.ImageSource {
	img, err := ui.DecodeImage(logoPNG)
	if err != nil {
		panic("invalid embedded PanReel logo: " + err.Error())
	}
	return img
}()

func brandMark(c *ui.Context, size float32) ui.Element {
	return ui.Image(c, logoImage).Size(size, size).Fit(ui.Contain).Shrink(0).Label("PanReel")
}
