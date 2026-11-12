package viewer

import (
	"slices"
	"time"

	"onon/internal/config"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// documentSwitch is the document of the tab shown before another, drawn
// going as the new one comes in, as motion's document says.
type documentSwitch struct {
	tex     *sdl.Texture // the document as it was last drawn, over the whole window
	rect    sdl.FRect    // the document's part of the window then
	at      time.Time    // when the other tab was shown; zero until it is
	forward bool         // the tab shown is after the one before
}

// captureDocument draws the document shown into a texture, for it to be
// drawn going once another tab is shown. Shown again mid-way, it is
// captured as it is on screen, the one before still going over it, so a
// quick run of switches flows on from where each was.
func (a *App) captureDocument() {
	m := &a.motion
	anim := a.anim(config.MotionDocument, config.MomentChange)
	if a.renderer == nil || m.frame == 0 || !anim.Moves() || a.motionScale() <= 0 || a.winW <= 0 || a.winH <= 0 {
		a.dropDocumentSwitch()
		return
	}
	r := a.renderer
	tex := sdl.CreateTexture(r, sdl.PixelFormatRGBA32, sdl.TextureAccessTarget, int32(a.winW), int32(a.winH))
	if tex == nil {
		return
	}
	sdl.SetTextureBlendMode(tex, sdl.BlendModeBlend)
	target := sdl.GetRenderTarget(r)
	if !sdl.SetRenderTarget(r, tex) {
		sdl.DestroyTexture(tex)
		a.dropDocumentSwitch()
		return
	}
	sdl.SetRenderDrawColor(r, 0, 0, 0, 0)
	sdl.RenderClear(r)
	rect := a.documentRect()
	bg := a.backgroundColor()
	_ = fillRect(r, rect, bg)
	loader, redraw, now, animating := a.loaderVisible, a.pendingRedraw, m.now, m.animating
	m.now = time.Now() // as it is now, between frames
	a.drawDocument(r)
	a.loaderVisible, a.pendingRedraw, m.now, m.animating = loader, redraw, now, animating
	sdl.SetRenderTarget(r, target)
	a.dropDocumentSwitch()
	m.documentSwitch = &documentSwitch{tex: tex, rect: rect}
}

// startDocumentSwitch starts the captured document going, the tab now
// shown at index to, after the one before at from or not.
func (a *App) startDocumentSwitch(from, to int) {
	s := a.motion.documentSwitch
	if s == nil {
		return
	}
	s.at, s.forward = time.Now(), from < 0 || to > from
	a.pendingRedraw = true
}

// dropDocumentSwitch frees the document drawn going.
func (a *App) dropDocumentSwitch() {
	m := &a.motion
	if m.documentSwitch != nil && m.documentSwitch.tex != nil {
		sdl.DestroyTexture(m.documentSwitch.tex)
	}
	m.documentSwitch = nil
}

// documentSwitchLayers are how the document coming in and the one going
// are drawn, and whether one is going.
func (a *App) documentSwitchLayers() (in, out layer, rect sdl.FRect, going bool) {
	s := a.motion.documentSwitch
	if s == nil || s.at.IsZero() {
		return restLayer, restLayer, sdl.FRect{}, false
	}
	anim := a.anim(config.MotionDocument, config.MomentChange)
	elapsed := a.unscaled(a.motion.now.Sub(s.at))
	// Going forward, the one before goes towards the start of the bar and
	// the new one comes from its end; going back, the other way.
	outSide, inSide := "left", "right"
	if a.tabBarVertical() {
		outSide, inSide = "top", "bottom"
	}
	outAnim, inAnim := anim, mirrorMoves(anim)
	if !s.forward {
		outSide, inSide = inSide, outSide
		outAnim, inAnim = inAnim, outAnim
	}
	rect = a.documentRect()
	auto := float64(rect.W) // a slide takes it out of the document's part
	if a.tabBarVertical() {
		auto = float64(rect.H)
	}
	out, done := a.layerAt(outAnim, elapsed, false, layerCtx{rect: rect, side: outSide, auto: auto})
	if done {
		a.dropDocumentSwitch()
		return restLayer, restLayer, rect, false
	}
	inAnim.Tracks = slices.DeleteFunc(slices.Clone(inAnim.Tracks), func(t config.Track) bool { return t.Prop == "opacity" })
	in, _ = a.layerAt(inAnim, elapsed, true, layerCtx{rect: rect, side: inSide, auto: auto})
	return in, out, rect, true
}

// mirrorMoves is anim with its moves across and down the other way.
func mirrorMoves(anim config.Animation) config.Animation {
	tracks := slices.Clone(anim.Tracks)
	for i, t := range tracks {
		if t.Prop == "x" || t.Prop == "y" {
			tracks[i].From, tracks[i].To = -t.From, -t.To
		}
	}
	anim.Tracks = tracks
	return anim
}

// drawDocument draws the document shown, coming in as another tab's
// goes, and the one going over it.
func (a *App) drawDocument(renderer *sdl.Renderer) {
	in, out, rect, going := a.documentSwitchLayers()
	draw := func() {
		a.withDocumentViewport(renderer, func() {
			a.drawPages(renderer)
			// An on-screen loader animates, so it asks for the next frame.
			a.pendingRedraw = a.loaderVisible
			a.drawLinkHints(renderer)
		})
	}
	if !going {
		draw()
		return
	}
	_ = a.drawLayer(in, rect, func() error { draw(); return nil })
	s := a.motion.documentSwitch
	_ = a.withClip(renderer, rect, func() error {
		return a.drawLayer(out, rect, func() error {
			sdl.SetTextureAlphaModFloat(s.tex, float32(1-a.motion.fade))
			full := sdl.FRect{W: float32(a.winW), H: float32(a.winH)}
			sdl.RenderTexture(renderer, s.tex, nil, &full)
			return nil
		})
	})
	a.motion.animating = true
}
