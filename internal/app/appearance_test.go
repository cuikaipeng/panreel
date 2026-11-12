package app

import (
	"image/color"
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

// Exercise the rendered controls, including hover/press and inactive windows.
// This catches a lost foreground color or a white hover fill, not just tokens
// that look correct in the theme but get overridden while painting.
func TestEnamelControlsRemainReadableAcrossInteractionStates(t *testing.T) {
	for _, theme := range []string{"light", "dark"} {
		for _, lang := range []string{"zh-Hans", "en"} {
			t.Run(theme+"/"+lang, func(t *testing.T) {
				a := newApp()
				a.prefs.Theme, a.prefs.Language = theme, lang
				a.online = true
				a.settings.Channels = []string{"example_channel"}
				tt := ui.NewTester(a.view, 960, 640)
				ink, tabInk := lightPalette.onAccent, mistBlueInk
				if theme == "dark" {
					ink, tabInk = darkPalette.onAccent, mistBlueDark
				}
				for _, control := range []struct {
					role ui.Role
					name string
					ink  ui.Color
				}{{ui.RoleButton, a.tr("搜索"), ink}, {ui.RoleTab, a.tr("探索"), tabInk}} {
					initial, ok := tt.AccessByRole(control.role, control.name)
					if !ok {
						t.Fatal("control missing", control.name)
					}
					x, y := initial.Bounds.X+initial.Bounds.W/2, initial.Bounds.Y+initial.Bounds.H/2
					for _, state := range []string{"normal", "hover", "pressed", "inactive"} {
						switch state {
						case "hover":
							tt.Move(x, y)
						case "pressed":
							tt.Press(x, y)
						case "inactive":
							tt.CancelPress()
							tt.SetWindowActive(false)
						}
						current, ok := tt.AccessByRole(control.role, control.name)
						if !ok || current.Bounds != initial.Bounds || current.Offscreen {
							t.Fatalf("%s %s changed layout or disappeared", control.name, state)
						}
						img, r := tt.Image(), current.Bounds
						visibleInk := 0
						for py := int(r.Y + 5); py < int(r.Y+r.H-5); py++ {
							for px := int(r.X + r.W/3); px < int(r.X+r.W-8); px++ {
								pixel := img.RGBAAt(px, py)
								distance := math.Abs(float64(pixel.R)-float64(control.ink.R)) + math.Abs(float64(pixel.G)-float64(control.ink.G)) + math.Abs(float64(pixel.B)-float64(control.ink.B))
								if distance < 35 {
									visibleInk++
								}
							}
						}
						if visibleInk < 8 {
							t.Fatalf("%s %s lost its readable foreground", control.name, state)
						}
						if control.role == ui.RoleButton {
							bg := img.RGBAAt(int(r.X+8), int(r.Y+r.H/2))
							fg := color.RGBA{R: ink.R, G: ink.G, B: ink.B, A: 255}
							if ratio := renderedContrast(fg, bg); ratio < 4.5 {
								t.Fatalf("%s primary label contrast %.2f:1", state, ratio)
							}
						}
					}
					tt.SetWindowActive(true)
					tt.Move(0, 100)
				}
			})
		}
	}
}

func renderedContrast(a, b color.RGBA) float64 {
	luminance := func(c color.RGBA) float64 {
		linear := func(v uint8) float64 {
			s := float64(v) / 255
			if s <= .04045 {
				return s / 12.92
			}
			return math.Pow((s+.055)/1.055, 2.4)
		}
		return .2126*linear(c.R) + .7152*linear(c.G) + .0722*linear(c.B)
	}
	x, y := luminance(a), luminance(b)
	return (math.Max(x, y) + .05) / (math.Min(x, y) + .05)
}
