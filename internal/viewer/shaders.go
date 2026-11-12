package viewer

import (
	"fmt"
	"image/color"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unsafe"

	"onon/internal/config"
	"onon/internal/shader"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// A theme's shader is a GLSL fragment shader drawn in an element's box,
// under its fill, with what is behind the box to draw from. SDL runs one
// only on its gpu renderer. A shader that samples what is behind it needs
// the frame drawn into a texture of its own, which its box is copied out
// of and which is drawn to the window once done; so a frame is drawn that
// way only while such a shader is in view. Other renderers draw the
// element without its shader.
//
// Shaders are kept cheap: they compile off the drawing thread, as soon as
// the theme is loaded, and are cached on disk; each box drawn in a frame
// has its uniforms in a render state of its own, so boxes are not flushed
// to the GPU one at a time; and a shader that reads iTime animates no
// faster than shader_fps, and not at all in a window that cannot be seen.

// SDL's functions for a fragment shader of one's own, which the binding
// leaves out.
var (
	sdlCreateGPURenderState              func(*sdl.Renderer, *sdl.GPURenderStateCreateInfo) unsafe.Pointer
	sdlSetGPURenderStateFragmentUniforms func(unsafe.Pointer, uint32, unsafe.Pointer, uint32) bool
	sdlSetGPURenderState                 func(*sdl.Renderer, unsafe.Pointer) bool
	sdlDestroyGPURenderState             func(unsafe.Pointer)
	sdlComposeCustomBlendMode            func(sdl.BlendFactor, sdl.BlendFactor, sdl.BlendOperation, sdl.BlendFactor, sdl.BlendFactor, sdl.BlendOperation) sdl.BlendMode
)

// renderStateSymbols loads them, reporting whether the SDL in use has them.
var renderStateSymbols = sync.OnceValue(func() bool {
	for name, dst := range map[string]any{
		"SDL_CreateGPURenderState":              &sdlCreateGPURenderState,
		"SDL_SetGPURenderStateFragmentUniforms": &sdlSetGPURenderStateFragmentUniforms,
		"SDL_SetGPURenderState":                 &sdlSetGPURenderState,
		"SDL_DestroyGPURenderState":             &sdlDestroyGPURenderState,
		"SDL_ComposeCustomBlendMode":            &sdlComposeCustomBlendMode,
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
	shader *sdl.GPUShader
	usage  shader.Usage
	// states are render states for the shader, one for each box it draws
	// in a frame: a state's uniforms are sent when the frame is, so boxes
	// sharing one would each have to be sent on their own.
	states []unsafe.Pointer
	frame  uint64 // the frame used counts states for
	used   int
}

// state is a render state for the next box the program draws in frame.
func (p *shaderProgram) state(renderer *sdl.Renderer, frame uint64) unsafe.Pointer {
	if p.frame != frame {
		p.frame, p.used = frame, 0
	}
	if p.used == len(p.states) {
		state := sdlCreateGPURenderState(renderer, &sdl.GPURenderStateCreateInfo{FragmentShader: p.shader})
		if state == nil {
			return nil
		}
		p.states = append(p.states, state)
	}
	p.used++
	return p.states[p.used-1]
}

// shaderCompiled is a shader compiled off the drawing thread.
type shaderCompiled struct {
	gen     uint64
	path    string
	format  sdl.GPUShaderFormat
	program shader.Program
	err     error
}

// shaderService runs the theme's shaders.
type shaderService struct {
	// programs are the shaders by file, nil for one that failed; those
	// being compiled are in compiling, until they are found in done.
	programs  map[string]*shaderProgram
	compiling map[string]bool
	gen       uint64 // counts resets, so a compile from before one is dropped
	mu        sync.Mutex
	done      []shaderCompiled
	// active is whether shaders are drawn in the frame being drawn.
	active bool
	// frame is the texture a frame is drawn into, and framing whether the
	// frame being drawn is. It is while needed, which says the last frame
	// drew a shader that samples what is behind it, and missed, that a
	// frame not drawn into it had such a shader come into view.
	frame          *sdl.Texture
	frameW, frameH int32
	framing        bool
	needed, missed bool
	sampled        bool // a shader sampled its backdrop this frame
	lastNeeded     time.Time
	ended          bool
	// backdrops are the textures boxes are copied into, by size, with the
	// frame each was last used on; blank is the texture drawn under a
	// shader that samples nothing.
	backdrops map[[2]int32]*shaderBackdrop
	blank     *sdl.Texture
	count     uint64
	// animated is whether a shader drawn this frame reads the time, and
	// animating whether one in the last frame did, drawn at animatedAt.
	animated, animating bool
	animatedAt          time.Time
	// time is iTime for the frame. It stands still while shaders are held,
	// from heldAt, and held is how long they have been held in all.
	time   float32
	held   time.Duration
	heldAt time.Time
	noted  bool // the renderer was said not to run shaders
}

type shaderBackdrop struct {
	texture *sdl.Texture
	used    uint64
}

// frameKeep is how long the frame's texture is kept after the last frame
// that needed it, so a menu opened again soon does not make it again.
const frameKeep = 5 * time.Second

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

// shadersOffNote says that the theme's shaders are off, for when it is
// chosen, so it is not left looking plain without a word; "" when there is
// nothing to say.
func (a *App) shadersOffNote() string {
	if a.config.Shaders || !a.themeHasShaders() {
		return ""
	}
	return "its shaders are off (onon.options.shaders)"
}

// shaderFPS is the most frames a second shaders animate at now: none in a
// window that cannot be seen, and fewer, or none, in one without focus.
func (a *App) shaderFPS() int {
	if a.window != nil {
		flags := sdl.GetWindowFlags(a.window)
		if flags&(sdl.WindowHidden|sdl.WindowMinimized|sdl.WindowOccluded) != 0 {
			return 0
		}
		if flags&sdl.WindowInputFocus == 0 {
			return a.config.ShaderFPSUnfocused
		}
	}
	return a.config.ShaderFPS
}

// shaderTime is iTime at now: seconds since onon started, less the time
// shaders were held still, so they go on from where they stopped.
func (s *shaderService) shaderTime(now time.Time, held bool) float32 {
	switch {
	case held && s.heldAt.IsZero():
		s.heldAt = now
	case !held && !s.heldAt.IsZero():
		s.held += now.Sub(s.heldAt)
		s.heldAt = time.Time{}
	}
	if held {
		now = s.heldAt
	}
	return float32((now.Sub(startTime) - s.held).Seconds())
}

// shaderDeadline is when the next frame of an animated shader is due, or
// zero when none is.
func (a *App) shaderDeadline() time.Time {
	s := &a.shaders
	if !s.animating {
		return time.Time{}
	}
	fps := a.shaderFPS()
	if fps <= 0 {
		return time.Time{}
	}
	return s.animatedAt.Add(time.Second / time.Duration(fps))
}

// stepShaderAnimation asks for a frame when an animated shader's is due.
func (a *App) stepShaderAnimation() {
	if due := a.shaderDeadline(); !due.IsZero() && !time.Now().Before(due) {
		a.pendingRedraw = true
	}
}

// beginShaderFrame starts the theme's shaders compiling, and draws the
// frame into a texture if a shader in view samples what is behind it.
func (a *App) beginShaderFrame() {
	s := &a.shaders
	s.count++
	s.animated, s.sampled, s.missed, s.ended, s.active = false, false, false, false, false
	if !a.config.Shaders || !a.requestThemeShaders() {
		s.releaseTextures()
		return
	}
	if !a.shadersAvailable() {
		if !s.noted {
			s.noted = true
			name := "no"
			if a.renderer != nil {
				name = "the " + sdl.GetRendererName(a.renderer)
			}
			a.logf("%s renderer runs no shaders; the theme's are left out", name)
			if a.config.Renderer == "auto" {
				// Shaders were turned on after the renderer was chosen.
				a.message = "restart onon to run theme shaders"
			}
		}
		return
	}
	s.active = true
	now := time.Now()
	s.time = s.shaderTime(now, a.shaderFPS() == 0)
	if !s.needed {
		if s.frame != nil && now.Sub(s.lastNeeded) > frameKeep {
			sdl.DestroyTexture(s.frame)
			s.frame = nil
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
// drawn into one, and notes what the next frame needs.
func (a *App) endShaderFrame() {
	s := &a.shaders
	if s.ended {
		return
	}
	s.ended = true
	if s.framing {
		s.framing = false
		sdl.SetRenderTarget(a.renderer, nil)
		sdl.RenderTexture(a.renderer, s.frame, nil, nil)
	}
	for size, b := range s.backdrops {
		if b.used != s.count {
			sdl.DestroyTexture(b.texture)
			delete(s.backdrops, size)
		}
	}
	now := time.Now()
	s.needed = s.sampled || s.missed
	if s.needed {
		s.lastNeeded = now
	}
	if s.animated {
		s.animatedAt = s.nextAnimatedAt(now, a.shaderFPS())
	}
	s.animating = s.animated
}

// nextAnimatedAt is when the frame drawn at now counts as drawn, which the
// next is timed from: when it was due, so frames keep to their rate rather
// than each coming as late as the wait for it ran over; or now, for one
// drawn early, as for something else, or one so late its time is gone.
func (s *shaderService) nextAnimatedAt(now time.Time, fps int) time.Time {
	if !s.animating || fps <= 0 {
		return now
	}
	interval := time.Second / time.Duration(fps)
	due := s.animatedAt.Add(interval)
	if due.After(now) || now.Sub(due) > interval {
		return now
	}
	return due
}

// drawShader draws st's shader, if it has one, over box, cut to shape.
func (a *App) drawShader(renderer *sdl.Renderer, st *config.Style, box sdl.Rect, shape boxShape) {
	s := &a.shaders
	if st.Shader.V == "" || !s.active || renderer != a.renderer {
		return
	}
	alpha := st.Opacity.V * (1 - a.motion.fade)
	if alpha <= 0 {
		return
	}
	program := a.shaderProgram(st.Shader.V)
	if program == nil {
		return // not compiled yet, or failed
	}
	samples := program.usage.Backdrop
	if samples && !s.framing {
		s.missed = true // drawn again at once, into a texture
		return
	}
	state := program.state(renderer, s.count)
	if state == nil {
		return
	}
	target := sdl.GetRenderTarget(renderer)
	// What the shader is drawn with: what is behind the box, for one that
	// samples it, or a blank texture.
	var under *sdl.Texture
	if samples {
		s.sampled = true
		if under = s.backdrop(renderer, box.W, box.H); under == nil || !a.copyBackdrop(renderer, under, target, box) {
			return
		}
	} else if under = s.blankTexture(renderer); under == nil {
		return
	}

	dst := sdl.FRect{X: float32(box.X), Y: float32(box.Y), W: float32(box.W), H: float32(box.H)}
	// The shader cuts itself to a rect or pill, its corners being in its
	// uniforms. A path it cannot know, so the shader is drawn into a
	// texture of its own, cut to the path's mask there, and then drawn.
	var cut *sdl.Texture
	if shape.Kind == "path" {
		if cut = s.backdrop(renderer, box.W, box.H); cut == nil || !sdl.SetRenderTarget(renderer, cut) {
			sdl.SetRenderTarget(renderer, target)
			return
		}
		sdl.SetRenderDrawColor(renderer, 0, 0, 0, 0)
		sdl.RenderClear(renderer)
		dst.X, dst.Y = 0, 0
	}

	uniforms := shader.Uniforms{
		Width: float32(box.W), Height: float32(box.H),
		Time:  s.time,
		Scale: a.px(1),
		Alt:   a.altColors,
	}
	if shape.Kind == "rect" {
		uniforms.Radius = shape.radius
	}
	data := uniforms.Bytes()
	sdlSetGPURenderStateFragmentUniforms(state, 0, unsafe.Pointer(&data[0]), uint32(len(data)))
	sdl.SetTextureAlphaMod(under, uint8(255*min(1, alpha)+0.5))
	sdlSetGPURenderState(renderer, state)
	sdl.RenderTexture(renderer, under, nil, &dst)
	sdlSetGPURenderState(renderer, nil)
	s.animated = s.animated || program.usage.Time
	if cut != nil {
		a.cutToShape(renderer, cut, target, box, shape)
	}
}

// copyBackdrop copies what the frame has behind box into backdrop, bottom
// up, as a shader's coordinates run, and goes back to drawing to target.
func (a *App) copyBackdrop(renderer *sdl.Renderer, backdrop, target *sdl.Texture, box sdl.Rect) bool {
	s := &a.shaders
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
	if !sdl.SetRenderTarget(renderer, backdrop) {
		return false
	}
	sdl.RenderTextureRotated(renderer, s.frame, &src, nil, 0, nil, sdl.FlipVertical)
	sdl.SetRenderTarget(renderer, target)
	return true
}

// blankTexture is a white pixel, drawn stretched under a shader that
// samples nothing, which is then drawn as any texture is, batched.
func (s *shaderService) blankTexture(renderer *sdl.Renderer) *sdl.Texture {
	if s.blank != nil {
		return s.blank
	}
	tex := sdl.CreateTexture(renderer, sdl.PixelFormatRGBA32, sdl.TextureAccessStatic, 1, 1)
	if tex == nil {
		return nil
	}
	white := [4]uint8{255, 255, 255, 255}
	sdl.UpdateTexture(tex, nil, unsafe.Pointer(&white[0]), 4)
	sdl.SetTextureBlendMode(tex, sdl.BlendModeBlend)
	s.blank = tex
	return tex
}

// cutToShape draws cut, a shader drawn into a texture box's size, at box
// in target, with what lies outside shape taken away. Drawn over a clear
// texture, the shader is there premultiplied by its alpha; the shape's
// mask then multiplies all four channels by its coverage.
func (a *App) cutToShape(renderer *sdl.Renderer, cut, target *sdl.Texture, box sdl.Rect, shape boxShape) {
	defer sdl.SetTextureBlendMode(cut, sdl.BlendModeBlend) // a backdrop again next time
	m, ok := a.shapeMask(renderer, maskSpec{shape: shape, w: box.W, h: box.H})
	if ok {
		keep := sdlComposeCustomBlendMode(
			sdl.BlendFactorZero, sdl.BlendFactorSrcAlpha, sdl.BlendOperationAdd,
			sdl.BlendFactorZero, sdl.BlendFactorSrcAlpha, sdl.BlendOperationAdd)
		if !sdl.SetTextureBlendMode(m.texture, keep) {
			ok = false
		} else {
			a.drawMask(renderer, maskSpec{shape: shape, w: box.W, h: box.H}, 0, 0, color.RGBA{R: 255, G: 255, B: 255, A: 255})
			sdl.SetTextureBlendMode(m.texture, sdl.BlendModeBlend)
		}
	}
	sdl.SetRenderTarget(renderer, target)
	if !ok {
		return // drawn uncut, the shader would cover what the shape leaves out
	}
	sdl.SetTextureBlendMode(cut, sdl.BlendModeBlendPremultiplied)
	sdl.SetTextureAlphaMod(cut, 255)
	dst := sdl.FRect{X: float32(box.X), Y: float32(box.Y), W: float32(box.W), H: float32(box.H)}
	sdl.RenderTexture(renderer, cut, nil, &dst)
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

// requestThemeShaders reports whether the theme has shaders, and where
// the renderer runs them starts compiling those not compiled, so they are
// ready by the time they are first drawn.
func (a *App) requestThemeShaders() bool {
	var paths []string
	for e := range config.ElementCount {
		if path := a.style(e).Shader.V; path != "" {
			paths = append(paths, path)
		}
	}
	if len(paths) > 0 && a.shadersAvailable() {
		for _, path := range paths {
			a.requestShader(path)
		}
	}
	return len(paths) > 0
}

// shaderProgram is the shader in the file path, or nil while it compiles
// or when it failed. One that fails is reported once and left out until
// the config changes.
func (a *App) shaderProgram(path string) *shaderProgram {
	if program, ok := a.shaders.programs[path]; ok {
		return program
	}
	a.requestShader(path)
	return nil
}

// requestShader starts compiling the shader in the file path, unless it
// is compiled or compiling.
func (a *App) requestShader(path string) {
	s := &a.shaders
	if _, ok := s.programs[path]; ok || s.compiling[path] {
		return
	}
	format, sdlFormat, err := a.shaderFormat()
	if err != nil {
		a.setShaderProgram(path, nil, err)
		return
	}
	file := path
	if !filepath.IsAbs(file) && a.config.ConfigPath != "" {
		file = filepath.Join(filepath.Dir(a.config.ConfigPath), file)
	}
	if s.compiling == nil {
		s.compiling = map[string]bool{}
	}
	s.compiling[path] = true
	gen, waker := s.gen, a.waker
	go func() {
		c := shaderCompiled{gen: gen, path: path, format: sdlFormat}
		source, err := os.ReadFile(file)
		if err == nil {
			c.program, err = shader.Compile(string(source), format)
		}
		c.err = err
		s.mu.Lock()
		s.done = append(s.done, c)
		s.mu.Unlock()
		waker.wake()
	}()
}

// pollShaderCompiles takes the shaders compiled since it last looked,
// making them programs for the renderer.
func (a *App) pollShaderCompiles() {
	s := &a.shaders
	s.mu.Lock()
	done := s.done
	s.done = nil
	s.mu.Unlock()
	for _, c := range done {
		if c.gen != s.gen {
			continue // compiled for a config since replaced
		}
		delete(s.compiling, c.path)
		var program *shaderProgram
		err := c.err
		if err == nil {
			program, err = a.loadShaderProgram(c)
		}
		a.setShaderProgram(c.path, program, err)
		a.pendingRedraw = true
	}
}

func (a *App) setShaderProgram(path string, program *shaderProgram, err error) {
	s := &a.shaders
	if s.programs == nil {
		s.programs = map[string]*shaderProgram{}
	}
	s.programs[path] = program
	if err != nil {
		a.reportThemeError("shader "+path, "theme shader "+filepath.Base(path), err)
	}
}

// shaderFormat is the form the renderer's GPU takes shaders in.
func (a *App) shaderFormat() (shader.Format, sdl.GPUShaderFormat, error) {
	device := sdl.GetGPURendererDevice(a.renderer)
	if device == nil {
		return 0, 0, fmt.Errorf("the renderer has no GPU device")
	}
	switch formats := sdl.GetGPUShaderFormats(device); {
	case formats&sdl.GPUShaderFormatSPIRV != 0:
		return shader.FormatSPIRV, sdl.GPUShaderFormatSPIRV, nil
	case formats&sdl.GPUShaderFormatMSL != 0:
		return shader.FormatMSL, sdl.GPUShaderFormatMSL, nil
	case formats&sdl.GPUShaderFormatDXIL != 0:
		return shader.FormatDXIL, sdl.GPUShaderFormatDXIL, nil
	}
	return 0, 0, fmt.Errorf("the %s GPU driver takes shaders in no form onon translates to", sdl.GetGPUDeviceDriver(device))
}

// loadShaderProgram gives the GPU a compiled shader.
func (a *App) loadShaderProgram(c shaderCompiled) (*shaderProgram, error) {
	device := sdl.GetGPURendererDevice(a.renderer)
	if device == nil {
		return nil, fmt.Errorf("the renderer has no GPU device")
	}
	if len(c.program.Code) == 0 {
		return nil, fmt.Errorf("no shader")
	}
	info := sdl.GPUShaderCreateInfo{Stage: sdl.GPUShaderStageFragment, NumSamplers: 1, NumUniformBuffers: 1, Format: c.format}
	info.SetEntryPoint(c.program.Entry)
	info.Code, info.CodeSize = &c.program.Code[0], uint64(len(c.program.Code))
	compiled := sdl.CreateGPUShader(device, &info)
	if compiled == nil {
		return nil, fmt.Errorf("%s", sdl.GetError())
	}
	program := &shaderProgram{shader: compiled, usage: c.program.Usage}
	// The first state is made now, so a shader SDL refuses is reported.
	if program.state(a.renderer, 0) == nil {
		err := fmt.Errorf("%s", sdl.GetError())
		sdl.ReleaseGPUShader(device, compiled)
		return nil, err
	}
	return program, nil
}

// reset forgets the shaders compiled, so their files are read again.
func (s *shaderService) reset(renderer *sdl.Renderer) {
	for _, program := range s.programs {
		if program == nil {
			continue
		}
		for _, state := range program.states {
			sdlDestroyGPURenderState(state)
		}
		if device := sdl.GetGPURendererDevice(renderer); device != nil {
			sdl.ReleaseGPUShader(device, program.shader)
		}
	}
	s.programs, s.compiling = nil, nil
	s.gen++
	s.mu.Lock()
	s.done = nil
	s.mu.Unlock()
	s.noted = false
}

// releaseTextures frees the frame's texture, the backdrops and the blank.
func (s *shaderService) releaseTextures() {
	if s.frame != nil {
		sdl.DestroyTexture(s.frame)
		s.frame = nil
	}
	if s.blank != nil {
		sdl.DestroyTexture(s.blank)
		s.blank = nil
	}
	for size, b := range s.backdrops {
		sdl.DestroyTexture(b.texture)
		delete(s.backdrops, size)
	}
	s.needed = false
}
