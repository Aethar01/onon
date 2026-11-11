package viewer

import (
	"errors"
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
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

func TestShaderDrawsWhatIsBehindItsBox(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "probe.glsl"), []byte(probeShader), 0o644); err != nil {
		t.Fatal(err)
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
	app.renderer = renderer
	app.winW, app.winH = 200, 200
	panel := &app.config.Theme.Elements[config.ElementPanel]
	panel.Shader = config.Opt[string]{V: filepath.Join(dir, "probe.glsl"), Set: true}
	panel.Fill = config.Opt[config.Color]{Set: true}
	panel.Shadow = config.Opt[config.Shadow]{Set: true}
	panel.BorderWidth = config.Opt[float64]{Set: true}
	panel.Radius = config.Opt[config.Corners]{V: config.Corners{30, 30, 30, 30}, Set: true}

	app.beginShaderFrame()
	if !app.shaders.framing {
		fmt.Println("the frame is not drawn into a texture:", sdl.GetError())
		return 1
	}
	fillRect(renderer, sdl.FRect{W: 200, H: 100}, color.RGBA{R: 255, A: 255})
	fillRect(renderer, sdl.FRect{Y: 100, W: 200, H: 100}, color.RGBA{G: 255, A: 255})
	st := app.style(config.ElementPanel)
	app.drawDefaultBox(renderer, &st, sdl.FRect{X: 50, Y: 50, W: 100, H: 100}, boxAll)
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
	app.endShaderFrame()
	if failed != 0 {
		fmt.Println("SDL:", sdl.GetError())
	}
	return failed
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
