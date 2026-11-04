package viewer

import (
	"image"
	"image/color"
	"math/rand/v2"
	"slices"
	"testing"

	"onon/internal/config"
)

func TestColorHelpersUseNormalAndAltPalettes(t *testing.T) {
	app := &App{
		tab: &tab{},
		config: config.Config{Theme: config.Theme{
			Palette: config.Palette{Background: [3]uint8{1, 2, 3}, Page: [3]uint8{4, 5, 6}, Foreground: [3]uint8{7, 8, 9}, StatusBar: [3]uint8{10, 11, 12}},
			Alt:     config.Palette{Background: [3]uint8{13, 14, 15}, Page: [3]uint8{16, 17, 18}, Foreground: [3]uint8{19, 20, 21}, StatusBar: [3]uint8{22, 23, 24}},
		}},
	}

	if app.statusVisible() {
		t.Fatal("expected status bar hidden in normal mode unless explicitly shown")
	}
	app.mode = modeCommand
	if !app.statusVisible() {
		t.Fatal("expected input mode to show status bar")
	}
	app.mode = modeNormal

	assertColor(t, app.backgroundColor(), color.RGBA{R: 1, G: 2, B: 3, A: 0xff})
	assertColor(t, app.pageBackgroundColor(), color.RGBA{R: 4, G: 5, B: 6, A: 0xff})
	assertColor(t, app.foregroundColor(), color.RGBA{R: 7, G: 8, B: 9, A: 0xff})
	assertColor(t, app.statusBarColor(), color.RGBA{R: 10, G: 11, B: 12, A: 0xff})

	app.altColors = true
	assertColor(t, app.backgroundColor(), color.RGBA{R: 13, G: 14, B: 15, A: 0xff})
	assertColor(t, app.pageBackgroundColor(), color.RGBA{R: 16, G: 17, B: 18, A: 0xff})
	assertColor(t, app.foregroundColor(), color.RGBA{R: 19, G: 20, B: 21, A: 0xff})
	assertColor(t, app.statusBarColor(), color.RGBA{R: 22, G: 23, B: 24, A: 0xff})
}

func TestRemapPageColorsPreservesAlphaAndMapsLuminance(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 3, 1))
	img.SetRGBA(0, 0, color.RGBA{R: 0, G: 0, B: 0, A: 0xff})
	img.SetRGBA(1, 0, color.RGBA{R: 255, G: 255, B: 255, A: 0xff})
	img.SetRGBA(2, 0, color.RGBA{R: 30, G: 40, B: 50, A: 0})

	remapPageColors(img, [3]uint8{200, 210, 220}, [3]uint8{10, 20, 30}, nil)

	assertColor(t, img.RGBAAt(0, 0), color.RGBA{R: 10, G: 20, B: 30, A: 0xff})
	assertColor(t, img.RGBAAt(1, 0), color.RGBA{R: 200, G: 210, B: 220, A: 0xff})
	assertColor(t, img.RGBAAt(2, 0), color.RGBA{R: 30, G: 40, B: 50, A: 0})
}

func assertColor(t *testing.T, got, want color.RGBA) {
	t.Helper()
	if got != want {
		t.Fatalf("got color %+v, want %+v", got, want)
	}
}

func TestRemapPageColorsKeepsImageAreas(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	red := color.RGBA{R: 255, A: 0xff}
	img.SetRGBA(0, 0, red)
	img.SetRGBA(1, 0, red)
	remapPageColors(img, [3]uint8{0, 0, 0}, [3]uint8{255, 255, 255}, []image.Rectangle{image.Rect(1, 0, 2, 1)})
	if img.RGBAAt(0, 0) == red {
		t.Fatal("pixel outside the image area was not remapped")
	}
	assertColor(t, img.RGBAAt(1, 0), red)
}

func TestRemapPageColorsMatchesPerPixelRemap(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	bg, fg := [3]uint8{30, 40, 50}, [3]uint8{220, 210, 200}
	for range 50 {
		bounds := image.Rect(rng.IntN(20)-10, rng.IntN(20)-10, 0, 0)
		bounds.Max = bounds.Min.Add(image.Pt(1+rng.IntN(40), 1+rng.IntN(40)))
		img := image.NewRGBA(bounds)
		for i := range img.Pix {
			img.Pix[i] = uint8(rng.IntN(256))
		}
		var keep []image.Rectangle
		for range rng.IntN(6) {
			min := image.Pt(bounds.Min.X+rng.IntN(50)-5, bounds.Min.Y+rng.IntN(50)-5)
			keep = append(keep, image.Rectangle{Min: min, Max: min.Add(image.Pt(rng.IntN(20), rng.IntN(20)))})
		}
		want := image.NewRGBA(bounds)
		copy(want.Pix, img.Pix)
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				px := want.Pix[want.PixOffset(x, y):][:4]
				if px[3] == 0 || slices.ContainsFunc(keep, image.Pt(x, y).In) {
					continue
				}
				lum := uint8((uint16(px[0])*77 + uint16(px[1])*150 + uint16(px[2])*29) >> 8)
				for c := range 3 {
					px[c] = mixChannel(fg[c], bg[c], lum)
				}
			}
		}
		remapPageColors(img, bg, fg, keep)
		if !slices.Equal(img.Pix, want.Pix) {
			t.Fatalf("bounds %v keep %v: remapped pixels differ from a per-pixel remap", bounds, keep)
		}
	}
}

func BenchmarkRemapPageColors(b *testing.B) {
	img := image.NewRGBA(image.Rect(0, 0, 1024, 1024))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	var keep []image.Rectangle
	for i := range 50 {
		keep = append(keep, image.Rect(i*20, i*20, i*20+100, i*20+60))
	}
	for b.Loop() {
		remapPageColors(img, [3]uint8{0, 0, 0}, [3]uint8{255, 255, 255}, keep)
	}
}
