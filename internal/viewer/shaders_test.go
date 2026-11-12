package viewer

import (
	"errors"
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"

	"onon/internal/config"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// shaderProbeEnv names the directory of the shader the test binary is to
// draw, run again as a probe: SDL opens windows only on the main thread,
// which TestMain has and a test does not.
const shaderProbeEnv = "ONON_TEST_SHADER_PROBE"

// shaderProbeSkip is the probe's exit code where there is no GPU renderer.
const shaderProbeSkip = 77

// A shader that keeps the red and green of what is behind it and adds blue.
const probeShader = `
void mainImage(out vec4 fragColor, in vec2 fragCoord) {
	vec4 behind = texture(iChannel0, fragCoord / iResolution.xy);
	fragColor = vec4(behind.rg, 1.0, 1.0);
}
`

// A shader that samples nothing: blue, and nothing of what is behind it.
const solidShader = `
void mainImage(out vec4 fragColor, in vec2 fragCoord) {
	fragColor = vec4(0.0, 0.0, 1.0, 1.0);
}
`

func TestShaderDrawsWhatIsBehindItsBox(t *testing.T) {
	dir := t.TempDir()
	for name, source := range map[string]string{"probe.glsl": probeShader, "solid.glsl": solidShader} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), shaderProbeEnv+"="+dir)
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == shaderProbeSkip {
		t.Skipf("no GPU renderer: %s", out)
	}
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

// runShaderProbe draws a box with the shader in dir over a window red
// above and green below, and checks what the shader made of each.
func runShaderProbe(dir string) int {
	if !sdl.Init(sdl.InitVideo) {
		fmt.Println("SDL:", sdl.GetError())
		return shaderProbeSkip
	}
	defer sdl.Quit()
	window := sdl.CreateWindow("onon shader probe", 200, 200, sdl.WindowHidden)
	if window == nil {
		fmt.Println("window:", sdl.GetError())
		return shaderProbeSkip
	}
	renderer := sdl.CreateRenderer(window, "gpu")
	if renderer == nil {
		fmt.Println("renderer:", sdl.GetError())
		return shaderProbeSkip
	}
	app := &App{tab: &tab{}, config: config.Default()}
	app.config.Shaders = true
	app.renderer = renderer
	app.winW, app.winH = 200, 200
	panel := &app.config.Theme.Elements[config.ElementPanel]
	panel.Shader = config.Opt[string]{V: filepath.Join(dir, "probe.glsl"), Set: true}
	panel.Fill = config.Opt[config.Color]{Set: true}
	panel.Shadow = config.Opt[config.Shadow]{Set: true}
	panel.BorderWidth = config.Opt[float64]{Set: true}
	panel.Radius = config.Opt[config.Corners]{V: config.Corners{30, 30, 30, 30}, Set: true}

	box := sdl.FRect{X: 50, Y: 50, W: 100, H: 100}
	st := app.style(config.ElementPanel)
	// The first frame starts the shader compiling; the next finds that it
	// samples what is behind it, so the one after is drawn into a texture.
	for frame := 0; !app.shaders.framing; frame++ {
		if frame == 3 {
			fmt.Println("the frame is not drawn into a texture:", app.message, sdl.GetError())
			return 1
		}
		app.endShaderFrame()
		if err := waitForShaders(app); err != nil {
			fmt.Println(err)
			return 1
		}
		app.beginShaderFrame()
		if !app.shaders.framing {
			app.drawDefaultBox(renderer, &st, box, boxAll)
		}
	}
	fillRect(renderer, sdl.FRect{W: 200, H: 100}, color.RGBA{R: 255, A: 255})
	fillRect(renderer, sdl.FRect{Y: 100, W: 200, H: 100}, color.RGBA{G: 255, A: 255})
	app.drawDefaultBox(renderer, &st, box, boxAll)
	if app.message != "" {
		fmt.Println(app.message)
		return 1
	}
	sdl.SetRenderTarget(renderer, app.shaders.frame)
	failed := 0
	for _, check := range []struct {
		what string
		x, y int32
		want [3]uint8
	}{
		{"above the box", 100, 20, [3]uint8{255, 0, 0}},
		{"below the box", 100, 180, [3]uint8{0, 255, 0}},
		{"the box's top, over red", 100, 70, [3]uint8{255, 0, 255}},
		{"the box's bottom, over green", 100, 130, [3]uint8{0, 255, 255}},
		{"the box's rounded corner", 52, 52, [3]uint8{255, 0, 0}},
	} {
		got, err := readPixel(renderer, check.x, check.y)
		if err != nil {
			fmt.Println(err)
			return 1
		}
		if got != check.want {
			fmt.Printf("%s: %v, want %v\n", check.what, got, check.want)
			failed = 1
		}
	}
	// A path the shader cannot cut itself to: a triangle over the box's
	// top left half, which the shader must leave the bottom right of.
	app.config.Theme.Elements[config.ElementPanel].Shape = config.Opt[config.Shape]{V: config.Shape{Kind: "path", Path: "M0,0 L100%,0 L0,100% Z"}, Set: true}
	app.styles = nil
	fillRect(renderer, sdl.FRect{W: 200, H: 200}, color.RGBA{R: 255, A: 255})
	st = app.style(config.ElementPanel)
	app.drawDefaultBox(renderer, &st, sdl.FRect{X: 50, Y: 50, W: 100, H: 100}, boxAll)
	sdl.SetRenderTarget(renderer, app.shaders.frame)
	for _, check := range []struct {
		what string
		x, y int32
		want [3]uint8
	}{
		{"inside the path", 70, 70, [3]uint8{255, 0, 255}},
		{"in the box, outside the path", 130, 130, [3]uint8{255, 0, 0}},
		{"beside the box", 20, 100, [3]uint8{255, 0, 0}},
	} {
		got, err := readPixel(renderer, check.x, check.y)
		if err != nil {
			fmt.Println(err)
			return 1
		}
		if got != check.want {
			fmt.Printf("path shape, %s: %v, want %v\n", check.what, got, check.want)
			failed = 1
		}
	}
	app.endShaderFrame()
	// A shader that samples nothing is drawn straight to the window, with
	// no frame drawn into a texture for it.
	app.config.Theme.Elements[config.ElementPanel].Shader = config.Opt[string]{V: filepath.Join(dir, "solid.glsl"), Set: true}
	app.config.Theme.Elements[config.ElementPanel].Shape = config.Opt[config.Shape]{V: config.Shape{Kind: "rect"}, Set: true}
	app.styles = nil
	st = app.style(config.ElementPanel)
	for frame := 0; frame < 3; frame++ {
		app.beginShaderFrame()
		if err := waitForShaders(app); err != nil {
			fmt.Println(err)
			return 1
		}
		fillRect(renderer, sdl.FRect{W: 200, H: 200}, color.RGBA{R: 255, A: 255})
		app.drawDefaultBox(renderer, &st, box, boxAll)
		if frame < 2 {
			app.endShaderFrame()
		}
	}
	if app.shaders.framing || app.shaders.frame != nil && app.shaders.needed {
		fmt.Println("a frame is drawn into a texture for a shader that samples nothing")
		failed = 1
	}
	for _, check := range []struct {
		what string
		x, y int32
		want [3]uint8
	}{
		{"in the solid shader's box", 100, 100, [3]uint8{0, 0, 255}},
		{"beside it", 20, 100, [3]uint8{255, 0, 0}},
	} {
		got, err := readPixel(renderer, check.x, check.y)
		if err != nil {
			fmt.Println(err)
			return 1
		}
		if got != check.want {
			fmt.Printf("solid shader, %s: %v, want %v\n", check.what, got, check.want)
			failed = 1
		}
	}
	app.endShaderFrame()
	if failed != 0 {
		fmt.Println("SDL:", sdl.GetError())
	}
	return failed
}

// waitForShaders waits for the shaders app is compiling.
func waitForShaders(app *App) error {
	deadline := time.Now().Add(30 * time.Second)
	for {
		app.pollShaderCompiles()
		if len(app.shaders.compiling) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("shaders still compiling: %v", app.shaders.compiling)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// readPixel is the colour of the render target at x, y.
func readPixel(renderer *sdl.Renderer, x, y int32) ([3]uint8, error) {
	surface := sdl.RenderReadPixels(renderer, &sdl.Rect{X: x, Y: y, W: 1, H: 1})
	if surface == nil {
		return [3]uint8{}, fmt.Errorf("read pixels: %s", sdl.GetError())
	}
	defer sdl.DestroySurface(surface)
	converted := sdl.ConvertSurface(surface, sdl.PixelFormatRGBA32)
	if converted == nil {
		return [3]uint8{}, fmt.Errorf("convert pixels: %s", sdl.GetError())
	}
	defer sdl.DestroySurface(converted)
	return [3]uint8(unsafe.Slice((*uint8)(converted.Pixels), 4)), nil
}

func TestShadersOffIsNotedWhenTheThemeIsChosen(t *testing.T) {
	app := &App{tab: &tab{}, config: config.Default()}
	app.config.Theme.Elements[config.ElementPanel].Shader = config.Opt[string]{V: "liquid.glsl", Set: true}
	if note := app.shadersOffNote(); !strings.Contains(note, "onon.options.shaders") {
		t.Fatalf("note %q, want the shaders option named", note)
	}
	app.showConfigWarnings()
	if app.message != "" {
		t.Errorf("config warning %q: shaders being off is not a fault of the config", app.message)
	}
	app.beginShaderFrame()
	if app.shaders.framing {
		t.Error("the frame is drawn for shaders that are off")
	}
}

func TestNoNoteOfShadersWhenOnOrUnused(t *testing.T) {
	app := &App{tab: &tab{}, config: config.Default()}
	if note := app.shadersOffNote(); note != "" {
		t.Errorf("note %q for a theme with no shaders", note)
	}
	app.config.Shaders = true
	app.config.Theme.Elements[config.ElementPanel].Shader = config.Opt[string]{V: "liquid.glsl", Set: true}
	app.styles = nil
	if note := app.shadersOffNote(); note != "" {
		t.Errorf("note %q with shaders on", note)
	}
}

func TestHeldShadersGoOnFromWhereTheyStopped(t *testing.T) {
	var s shaderService
	at := startTime.Add(10 * time.Second)
	if got := s.shaderTime(at, false); got != 10 {
		t.Fatalf("iTime %v at 10s, want 10", got)
	}
	s.shaderTime(at, true)
	if got := s.shaderTime(at.Add(5*time.Second), true); got != 10 {
		t.Errorf("iTime %v while held, want 10", got)
	}
	if got := s.shaderTime(at.Add(5*time.Second), false); got != 10 {
		t.Errorf("iTime %v as it is let go after 5s held, want 10", got)
	}
	if got := s.shaderTime(at.Add(7*time.Second), false); got != 12 {
		t.Errorf("iTime %v 2s after 5s held, want 12", got)
	}
}

func TestAnimatedShaderFramesKeepToShaderFPS(t *testing.T) {
	app := &App{tab: &tab{}, config: config.Default()}
	if !app.shaderDeadline().IsZero() {
		t.Error("a frame is due with no animated shader")
	}
	drawn := time.Now()
	app.shaders.animating, app.shaders.animatedAt = true, drawn
	app.config.ShaderFPS = 20
	if got := app.shaderDeadline().Sub(drawn); got != 50*time.Millisecond {
		t.Errorf("next frame %v after the last at 20 fps, want 50ms", got)
	}
	app.stepShaderAnimation()
	if app.pendingRedraw {
		t.Error("a frame is drawn before it is due")
	}
	app.shaders.animatedAt = drawn.Add(-time.Second)
	app.stepShaderAnimation()
	if !app.pendingRedraw {
		t.Error("no frame is drawn once it is due")
	}
	app.config.ShaderFPS = 0
	if !app.shaderDeadline().IsZero() {
		t.Error("a frame is due with shaders held still")
	}
}

func TestAnimatedShaderFramesDoNotDrift(t *testing.T) {
	last := time.Now()
	s := shaderService{animating: true, animatedAt: last}
	// Drawn 3ms after it was due: the next is timed from when it was due.
	if got := s.nextAnimatedAt(last.Add(53*time.Millisecond), 20); !got.Equal(last.Add(50 * time.Millisecond)) {
		t.Errorf("a late frame counts from %v after the last, want 50ms", got.Sub(last))
	}
	// Drawn early, for something else: from then.
	if got := s.nextAnimatedAt(last.Add(10*time.Millisecond), 20); !got.Equal(last.Add(10 * time.Millisecond)) {
		t.Errorf("an early frame counts from %v after the last, want 10ms", got.Sub(last))
	}
	// So late that catching up would draw frames back to back: from then.
	if got := s.nextAnimatedAt(last.Add(time.Second), 20); !got.Equal(last.Add(time.Second)) {
		t.Errorf("a long-late frame counts from %v after the last, want 1s", got.Sub(last))
	}
}
