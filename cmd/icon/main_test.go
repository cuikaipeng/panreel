package main

import (
	"image"
	"image/color"
	"path/filepath"
	"testing"
)

func TestAppIconExportHasSolidInterior(t *testing.T) {
	source, err := readPNG("../../docs/brand/app-icon-source.png")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "icon.png")
	if err := writePNG(path, cleanIconAlpha(source), source.Bounds(), 1024, 1024); err != nil {
		t.Fatal(err)
	}
	icon, err := readPNG(path)
	if err != nil {
		t.Fatal(err)
	}
	if icon.Bounds() != image.Rect(0, 0, 1024, 1024) {
		t.Fatalf("unexpected icon size: %v", icon.Bounds())
	}
	// This area covers both the cloud and its tile, away from the contour.
	// The original export leaked alpha 253/254 into these painted surfaces.
	for y := 200; y < 820; y++ {
		for x := 200; x < 820; x++ {
			if _, _, _, a := icon.At(x, y).RGBA(); a != 0xffff {
				t.Fatalf("translucent interior at (%d, %d): %d", x, y, a)
			}
		}
	}
	for _, p := range []image.Point{{0, 0}, {1023, 0}, {0, 1023}, {1023, 1023}, {600, 970}} {
		if _, _, _, a := icon.At(p.X, p.Y).RGBA(); a != 0 {
			t.Fatalf("background debris at %v: alpha %d", p, a)
		}
	}
	// The silhouette must still have antialiasing; flattening all alpha would
	// merely replace the speckles with a square background or jagged edges.
	for y := 0; y < 1024; y++ {
		for x := 0; x < 1024; x++ {
			c := color.NRGBAModel.Convert(icon.At(x, y)).(color.NRGBA)
			if c.A > 0 && c.A < 255 {
				return
			}
		}
	}
	t.Fatal("icon lost its antialiased transparent contour")
}
