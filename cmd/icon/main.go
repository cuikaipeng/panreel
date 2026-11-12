// icon exports the approved Floating Cloud artwork for the UI and app bundles.
// Run from the repository root with pnpm icon or go run ./cmd/icon.
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"

	"golang.org/x/image/draw"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	mark, err := readPNG("docs/brand/logo-source.png")
	if err != nil {
		return err
	}
	// Tighten only the UI asset's transparent margins. Ignore imperceptible
	// alpha noise when measuring the artwork, but preserve its pixels/colors.
	bounds := image.Rectangle{}
	for y := mark.Bounds().Min.Y; y < mark.Bounds().Max.Y; y++ {
		for x := mark.Bounds().Min.X; x < mark.Bounds().Max.X; x++ {
			if _, _, _, alpha := mark.At(x, y).RGBA(); alpha > 16*257 {
				bounds = bounds.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	if bounds.Empty() {
		return fmt.Errorf("logo source contains no visible artwork")
	}
	bounds = bounds.Inset(-2).Intersect(mark.Bounds())
	const width = 256
	height := max(1, (bounds.Dy()*width+bounds.Dx()/2)/bounds.Dx())
	if err := writePNG("internal/app/assets/logo.png", mark, bounds, width, height); err != nil {
		return err
	}
	appIcon, err := readPNG("docs/brand/app-icon-source.png")
	if err != nil {
		return err
	}
	if appIcon.Bounds().Dx() != appIcon.Bounds().Dy() {
		return fmt.Errorf("app icon source must be square")
	}
	// MyGo derives the macOS ICNS and Windows icon resources from this PNG.
	appIcon = cleanIconAlpha(appIcon)
	return writePNG("resources/icon.png", appIcon, appIcon.Bounds(), 1024, 1024)
}

// cleanIconAlpha prepares the single, solid app-icon tile for system rendering.
// The generated source has 253/254 alpha throughout its painted interior and
// stray pixels outside the tile. Keep the RGB artwork and antialiased contour,
// but make near-opaque pixels solid and discard detached background debris.
func cleanIconAlpha(source image.Image) *image.NRGBA {
	b := source.Bounds()
	out := image.NewNRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(source.At(x, y)).(color.NRGBA)
			switch {
			case c.A <= 16:
				c = color.NRGBA{}
			case c.A >= 240:
				c.A = 255
			}
			out.SetNRGBA(x, y, c)
		}
	}

	w, h := b.Dx(), b.Dy()
	seen := make([]bool, w*h)
	var largest []int
	for start := range seen {
		if seen[start] || out.Pix[start*4+3] == 0 {
			continue
		}
		component := []int{start}
		seen[start] = true
		for head := 0; head < len(component); head++ {
			x, y := component[head]%w, component[head]/w
			for yy := max(0, y-1); yy <= min(h-1, y+1); yy++ {
				for xx := max(0, x-1); xx <= min(w-1, x+1); xx++ {
					i := yy*w + xx
					if !seen[i] && out.Pix[i*4+3] != 0 {
						seen[i] = true
						component = append(component, i)
					}
				}
			}
		}
		if len(component) > len(largest) {
			largest = component
		}
	}
	clear(seen)
	for _, i := range largest {
		seen[i] = true
	}
	for i, keep := range seen {
		if !keep {
			clear(out.Pix[i*4 : i*4+4])
		}
	}
	return out
}

func readPNG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

func writePNG(path string, source image.Image, bounds image.Rectangle, width, height int) error {
	output := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(output, output.Bounds(), source, bounds, draw.Src, nil)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	err = png.Encode(f, output)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return err
}
