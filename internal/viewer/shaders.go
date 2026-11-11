package viewer

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"onon/internal/config"
	"onon/internal/shader"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// A theme's shader is a GLSL fragment shader drawn in an element's box,
// under its fill, with what is behind the box to draw from. SDL runs one
// only on its gpu renderer, so while a theme has shaders the frame is
// drawn into a texture of its own, which a shader's box is copied out of
// for the shader to read, and which is drawn to the window once done.
// Other renderers draw the element without its shader.

// SDL's functions for a fragment shader of one's own, which the binding
// leaves out.
var (
	sdlCreateGPURenderState              func(*sdl.Renderer, *sdl.GPURenderStateCreateInfo) unsafe.Pointer
	sdlSetGPURenderStateFragmentUniforms func(unsafe.Pointer, uint32, unsafe.Pointer, uint32) bool
	sdlSetGPURenderState                 func(*sdl.Renderer, unsafe.Pointer) bool
	sdlDestroyGPURenderState             func(unsafe.Pointer)
)

// renderStateSymbols loads them, reporting whether the SDL in use has them.
var renderStateSymbols = sync.OnceValue(func() bool {
	for name, dst := range map[string]any{
		"SDL_CreateGPURenderState":              &sdlCreateGPURenderState,
		"SDL_SetGPURenderStateFragmentUniforms": &sdlSetGPURenderStateFragmentUniforms,
		"SDL_SetGPURenderState":                 &sdlSetGPURenderState,
		"SDL_DestroyGPURenderState":             &sdlDestroyGPURenderState,
	} {
		if err := loadSDLSymbol(name, dst); err != nil {
			log.Printf("shaders unavailable: %s: %v", name, err)
			return false
		}
	}
	return true
})

// shaderProgram is a theme's shader, compiled for the renderer.
type shaderProgram struct {
	state    unsafe.Pointer
	shader   *sdl.GPUShader
	animated bool // it reads the time, so it is drawn on every frame
}

// shaderService runs the theme's shaders.
type shaderService struct {
	// programs are the shaders by file, nil for one that failed.
	programs map[string]*shaderProgram
	// frame is the texture a frame is drawn into, and framing whether the
	// frame being drawn is.
	frame          *sdl.Texture
	frameW, frameH int32
	framing        bool
	// backdrops are the textures boxes are copied into, by size, with the
	// frame each was last used on.
	backdrops map[[2]int32]*shaderBackdrop
	count     uint64
	animated  bool // a shader drawn this frame reads the time
	noted     bool // the renderer was said not to run shaders
}

type shaderBackdrop struct {
	texture *sdl.Texture
	used    uint64
}

// shadersAvailable reports whether the renderer runs shaders.
func (a *App) shadersAvailable() bool {
	return a.renderer != nil && sdl.GetRendererName(a.renderer) == "gpu" && renderStateSymbols()
}

// themeHasShaders reports whether any element of the theme has a shader.
func (a *App) themeHasShaders() bool {
	for e := range config.ElementCount {
		if a.style(e).Shader.V != "" {
			return true
		}
	}
	return false
}

// beginShaderFrame draws the frame into a texture if the theme's shaders
// are to run on it.
func (a *App) beginShaderFrame() {
	s := &a.shaders
	s.count++
	s.animated = false
	if !a.themeHasShaders() {
		s.releaseTextures()
		return
	}
	if !a.shadersAvailable() {
		if !s.noted {
			s.noted = true
			a.logf("the %s renderer runs no shaders; the theme's are left out", sdl.GetRendererName(a.renderer))
		}
		return
	}
	w, h := int32(a.winW), int32(a.winH)
	if w <= 0 || h <= 0 {
		return
	}
	if s.frame == nil || s.frameW != w || s.frameH != h {
		if s.frame != nil {
			sdl.DestroyTexture(s.frame)
		}
		s.frame = sdl.CreateTexture(a.renderer, sdl.PixelFormatRGBA32, sdl.TextureAccessTarget, w, h)
		if s.frame == nil {
			return
		}
		s.frameW, s.frameH = w, h
		// It is copied whole, to a shader's backdrop and to the window.
		sdl.SetTextureBlendMode(s.frame, sdl.BlendModeNone)
		sdl.SetTextureScaleMode(s.frame, sdl.ScaleModeNearest)
	}
	s.framing = sdl.SetRenderTarget(a.renderer, s.frame)
}

// endShaderFrame draws the frame's texture to the window, if the frame was
// drawn into one.
func (a *App) endShaderFrame() {
	s := &a.shaders
	if !s.framing {
		return
	}
	s.framing = false
	sdl.SetRenderTarget(a.renderer, nil)
	sdl.RenderTexture(a.renderer, s.frame, nil, nil)
	for size, b := range s.backdrops {
		if b.used != s.count {
			sdl.DestroyTexture(b.texture)
			delete(s.backdrops, size)
		}
	}
	if s.animated {
		a.pendingRedraw = true
	}
}

// drawShader draws st's shader, if it has one, over box, cut to shape.
func (a *App) drawShader(renderer *sdl.Renderer, st *config.Style, box sdl.Rect, shape boxShape) {
	s := &a.shaders
	if st.Shader.V == "" || !s.framing || renderer != a.renderer {
		return
	}
	alpha := st.Opacity.V * (1 - a.motion.fade)
	if alpha <= 0 {
		return
	}
	program := a.shaderProgram(st.Shader.V)
	if program == nil {
		return
	}
	backdrop := s.backdrop(renderer, box.W, box.H)
	if backdrop == nil {
		return
	}
	// The box as it lies in the window, which the frame's texture covers.
	src := sdl.FRect{X: float32(box.X), Y: float32(box.Y), W: float32(box.W), H: float32(box.H)}
	m := &a.motion
	if m.hasViewport {
		src.X += float32(m.viewport.X)
		src.Y += float32(m.viewport.Y)
	}
	// A box drawn into a layer shown scaled is over what the scaled box
	// will cover, not what it would at rest.
	for i := len(m.scales) - 1; i >= 0; i-- {
		l := m.scales[i]
		src = sdl.FRect{X: l.x + (src.X-l.x)*l.k, Y: l.y + (src.Y-l.y)*l.k, W: src.W * l.k, H: src.H * l.k}
	}
	// What is behind the box, bottom up, as a shader's coordinates run.
	target := sdl.GetRenderTarget(renderer)
	if !sdl.SetRenderTarget(renderer, backdrop) {
		return
	}
	sdl.RenderTextureRotated(renderer, s.frame, &src, nil, 0, nil, sdl.FlipVertical)
	sdl.SetRenderTarget(renderer, target)

	uniforms := shader.Uniforms{
		Width: float32(box.W), Height: float32(box.H),
		Time:  float32(time.Since(startTime).Seconds()),
		Scale: a.px(1),
		Alt:   a.altColors,
	}
	if shape.Kind == "rect" {
		uniforms.Radius = shape.radius
	}
	data := uniforms.Bytes()
	sdlSetGPURenderStateFragmentUniforms(program.state, 0, unsafe.Pointer(&data[0]), uint32(len(data)))
	sdl.SetTextureAlphaMod(backdrop, uint8(255*min(1, alpha)+0.5))
	sdlSetGPURenderState(renderer, program.state)
	dst := sdl.FRect{X: float32(box.X), Y: float32(box.Y), W: float32(box.W), H: float32(box.H)}
	sdl.RenderTexture(renderer, backdrop, nil, &dst)
	sdlSetGPURenderState(renderer, nil)
	// The uniforms are the state's, so what was drawn with them is sent
	// before the next box changes them.
	sdl.FlushRenderer(renderer)
	s.animated = s.animated || program.animated
}

// backdrop is a texture w by h for a box's backdrop.
func (s *shaderService) backdrop(renderer *sdl.Renderer, w, h int32) *sdl.Texture {
	size := [2]int32{w, h}
	if b, ok := s.backdrops[size]; ok && b.used != s.count {
		b.used = s.count
		return b.texture
	}
	// A size used twice in a frame takes a texture of its own, each being
	// drawn from only once the frame is sent.
	for ; s.backdrops[size] != nil; size[0] += 1 << 16 {
	}
	tex := sdl.CreateTexture(renderer, sdl.PixelFormatRGBA32, sdl.TextureAccessTarget, w, h)
	if tex == nil {
		return nil
	}
	sdl.SetTextureBlendMode(tex, sdl.BlendModeBlend)
	sdl.SetTextureScaleMode(tex, sdl.ScaleModeLinear)
	if s.backdrops == nil {
		s.backdrops = map[[2]int32]*shaderBackdrop{}
	}
	s.backdrops[size] = &shaderBackdrop{texture: tex, used: s.count}
	return tex
}

// shaderProgram is the shader in the file path, compiled on first use. One
// that fails is reported once and left out until the config changes.
func (a *App) shaderProgram(path string) *shaderProgram {
	s := &a.shaders
	if program, ok := s.programs[path]; ok {
		return program
	}
	if s.programs == nil {
		s.programs = map[string]*shaderProgram{}
	}
	program, err := a.compileShader(path)
	if err != nil {
		a.reportThemeError("shader "+path, "theme shader "+filepath.Base(path), err)
	}
	s.programs[path] = program
	return program
}

func (a *App) compileShader(path string) (*shaderProgram, error) {
	file := path
	if !filepath.IsAbs(file) && a.config.ConfigPath != "" {
		file = filepath.Join(filepath.Dir(a.config.ConfigPath), file)
	}
	source, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	spirv, err := shader.SPIRV(shader.Wrap(string(source)))
	if err != nil {
		return nil, err
	}
	device := sdl.GetGPURendererDevice(a.renderer)
	if device == nil {
		return nil, fmt.Errorf("the renderer has no GPU device")
	}
	info := sdl.GPUShaderCreateInfo{Stage: sdl.GPUShaderStageFragment, NumSamplers: 1, NumUniformBuffers: 1}
	var format shader.Format
	switch formats := sdl.GetGPUShaderFormats(device); {
	case formats&sdl.GPUShaderFormatSPIRV != 0:
		format, info.Format = shader.FormatSPIRV, sdl.GPUShaderFormatSPIRV
	case formats&sdl.GPUShaderFormatMSL != 0:
		format, info.Format = shader.FormatMSL, sdl.GPUShaderFormatMSL
	case formats&sdl.GPUShaderFormatDXIL != 0:
		format, info.Format = shader.FormatDXIL, sdl.GPUShaderFormatDXIL
	default:
		return nil, fmt.Errorf("the %s GPU driver takes shaders in no form onon translates to", sdl.GetGPUDeviceDriver(device))
	}
	code, entry, err := shader.Translate(spirv, format)
	if err != nil {
		return nil, err
	}
	info.SetEntryPoint(entry)
	info.Code, info.CodeSize = &code[0], uint64(len(code))
	compiled := sdl.CreateGPUShader(device, &info)
	if compiled == nil {
		return nil, fmt.Errorf("%s", sdl.GetError())
	}
	state := sdlCreateGPURenderState(a.renderer, &sdl.GPURenderStateCreateInfo{FragmentShader: compiled})
	if state == nil {
		err := fmt.Errorf("%s", sdl.GetError())
		sdl.ReleaseGPUShader(device, compiled)
		return nil, err
	}
	return &shaderProgram{state: state, shader: compiled, animated: strings.Contains(string(source), "iTime")}, nil
}

// reset forgets the shaders compiled, so their files are read again.
func (s *shaderService) reset(renderer *sdl.Renderer) {
	for _, program := range s.programs {
		if program == nil {
			continue
		}
		sdlDestroyGPURenderState(program.state)
		if device := sdl.GetGPURendererDevice(renderer); device != nil {
			sdl.ReleaseGPUShader(device, program.shader)
		}
	}
	s.programs = nil
	s.noted = false
}

// releaseTextures frees the frame's texture and the backdrops.
func (s *shaderService) releaseTextures() {
	if s.frame != nil {
		sdl.DestroyTexture(s.frame)
		s.frame = nil
	}
	for size, b := range s.backdrops {
		sdl.DestroyTexture(b.texture)
		delete(s.backdrops, size)
	}
}
