package viewer

import (
	"math"
	"slices"
	"sync/atomic"
	"time"

	"onon/internal/config"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// motionState is the UI's motion in progress. Drawing code asks where a
// value heading for its target is now, or how something entering, leaving
// or changing is drawn now, and frames keep coming while any still moves.
type motionState struct {
	frame     uint64    // frames drawn
	now       time.Time // when the frame being drawn is drawn
	animating bool      // something is still moving this frame
	tweens    map[tweenKey]*tween
	plays     map[tweenKey]*play
	changes   map[tweenKey]*changeState
	fade      float64 // how far what is drawn now is faded out, from 0 to 1
	// shownView is the menu drawn last frame, shownPanels the panels, and
	// closing those drawn leaving after they closed.
	shownView   *uiView
	shownPanels []*uiView
	closing     []*uiView
	// fadingText is the last status text shown, drawn leaving once cleared.
	fadingText string
	// viewport is the renderer's viewport while drawing is moved, unless
	// hasViewport is false for the whole window; layers are the textures scaled layers are drawn
	// into, one for each depth of them, and layerDepth how deep it is.
	viewport    sdl.Rect
	hasViewport bool
	layers      []*sdl.Texture
	layerDepth  int
	layersUsed  uint64      // the last frame a scaled layer was drawn
	osReduced   atomic.Bool // the OS asks for less motion; see reducedMotion
	// osReading is whether the OS's setting is being read, and osReadAt
	// when it was last read; see refreshReducedMotion.
	osReading atomic.Bool
	osReadAt  time.Time
}

// tweenKey names something that moves: what it is, and the view or text
// it belongs to, if any.
type tweenKey struct {
	kind string
	view *uiView
	text string
}

// tween is a value moving from one place to another.
type tween struct {
	from, to, value float64
	start           time.Time
	seen            uint64 // the last frame the value was asked for
}

// play is an animation playing from when it was first asked for.
type play struct {
	start time.Time
	seen  uint64
}

// changeState is what a changing thing, such as the selection, is now and
// was before, and when it changed.
type changeState struct {
	id, prev int
	at       time.Time // zero until it first changes
	seen     uint64
}

// beginMotionFrame starts a frame's motion.
func (a *App) beginMotionFrame() {
	m := &a.motion
	m.frame++
	m.now = time.Now()
	m.animating = false
	m.fade = 0
	a.releaseLayers()
	// What is not asked for is dropped now and then, and whenever there
	// is much, so a closed view is not kept by its keys.
	if len(m.tweens)+len(m.plays)+len(m.changes) > 64 || m.frame%64 == 0 {
		for key, tw := range m.tweens {
			if tw.seen+1 < m.frame {
				delete(m.tweens, key)
			}
		}
		for key, p := range m.plays {
			if p.seen+1 < m.frame {
				delete(m.plays, key)
			}
		}
		for key, c := range m.changes {
			if c.seen+1 < m.frame {
				delete(m.changes, key)
			}
		}
	}
}

// reducedMotion reports whether motion is reduced: the OS asks for less,
// or the theme says to.
func (a *App) reducedMotion() bool {
	return a.motion.osReduced.Load() || a.config.Theme.Motion.ForceReduced
}

// motionScale is how much slower than written everything moves: 0 for
// not at all.
func (a *App) motionScale() float64 {
	m := &a.config.Theme.Motion
	if a.reducedMotion() && m.Reduced.Scale.Set {
		return m.Reduced.Scale.V
	}
	return m.Scale
}

// anim is the theme's animation of target at moment, reduced or not.
func (a *App) anim(target config.MotionTarget, moment config.Moment) config.Animation {
	return a.config.Theme.Motion.Animation(target, moment, a.reducedMotion())
}

// viewAnim is anim with view's own motion over it.
func (a *App) viewAnim(view *uiView, target config.MotionTarget, moment config.Moment) config.Animation {
	anim := a.anim(target, moment)
	if view != nil && view.motion != nil {
		anim = view.motion[target][moment].Apply(anim)
	}
	return anim
}

// ease is e at fraction p of its time, calling a Lua easing.
func (a *App) ease(e config.Easing, p float64) float64 {
	if e.Func == nil || p <= 0 || p >= 1 {
		return e.Ease(p)
	}
	if a.runtime == nil {
		return p
	}
	v, err := a.runtime.RunEasing(e.Func, p)
	if err != nil {
		a.reportThemeError(e.Func, "theme motion easing", err)
		return p
	}
	return v
}

// scaledMs is d milliseconds at the motion's scale, as a duration.
func (a *App) scaledMs(d float64) time.Duration {
	return time.Duration(d * a.motionScale() * float64(time.Millisecond))
}

// animate returns where a value heading for target is now, moving as
// anim's timing says. A value not asked for last frame is already at its
// target.
func (a *App) animate(key tweenKey, target float64, anim config.Animation) float64 {
	return a.animateFrom(key, math.NaN(), target, anim)
}

// glide is animate for something that moves only if anim glides it.
func (a *App) glide(key tweenKey, target float64, anim config.Animation) float64 {
	if !anim.Glide {
		anim.Duration = 0
	}
	return a.animate(key, target, anim)
}

// animateFrom is animate for a value that, when not asked for last frame,
// starts from from.
func (a *App) animateFrom(key tweenKey, from, target float64, anim config.Animation) float64 {
	m := &a.motion
	tw := m.tweens[key]
	switch {
	case tw == nil || tw.seen+1 < m.frame:
		if math.IsNaN(from) {
			from = target
		}
		tw = &tween{from: from, to: target, value: from, start: m.now}
		if m.tweens == nil {
			m.tweens = map[tweenKey]*tween{}
		}
		m.tweens[key] = tw
	case tw.to != target:
		// A new target starts from the last one, not from where the value
		// is on its way: under a held key, which retargets faster than a
		// transition runs, the value then trails by a step at most rather
		// than ever further behind.
		tw.from, tw.to, tw.start = tw.to, target, m.now
	}
	tw.seen = m.frame
	duration := a.scaledMs(anim.Duration)
	progress := 1.0
	if duration > 0 {
		progress = float64(m.now.Sub(tw.start)-a.scaledMs(anim.Delay)) / float64(duration)
	}
	if progress >= 1 || tw.from == tw.to {
		tw.value = tw.to
	} else {
		tw.value = tw.from + (tw.to-tw.from)*a.ease(anim.Easing, max(0, progress))
		m.animating = true
	}
	return tw.value
}

// shiftTween moves the start of key's transition by d, if it began this
// frame, and returns where its value is now.
func (a *App) shiftTween(key tweenKey, d float64) float64 {
	tw := a.motion.tweens[key]
	if tw == nil {
		return math.NaN()
	}
	if d != 0 && tw.start.Equal(a.motion.now) && tw.value == tw.from && tw.from != tw.to {
		tw.from += d
		tw.value = tw.from
	}
	return tw.value
}

// played is how long key has played, in milliseconds as written: since
// it was first asked for, on one frame after another. With no motion it
// has played for ever.
func (a *App) played(key tweenKey) float64 {
	m := &a.motion
	p := m.plays[key]
	if p == nil || p.seen+1 < m.frame {
		p = &play{start: m.now}
		if m.plays == nil {
			m.plays = map[tweenKey]*play{}
		}
		m.plays[key] = p
	}
	p.seen = m.frame
	return a.unscaled(m.now.Sub(p.start))
}

// unscaled is d in milliseconds as written, at the motion's scale.
func (a *App) unscaled(d time.Duration) float64 {
	scale := a.motionScale()
	if scale <= 0 {
		return math.Inf(1)
	}
	return float64(d) / float64(time.Millisecond) / scale
}

// changed follows key's id, as the selected row's, returning the one
// before and how long ago, in milliseconds as written, it changed; ok is
// false before it first changes.
func (a *App) changed(key tweenKey, id int) (prev int, since float64, ok bool) {
	m := &a.motion
	c := m.changes[key]
	switch {
	case c == nil || c.seen+1 < m.frame:
		c = &changeState{id: id, prev: id}
		if m.changes == nil {
			m.changes = map[tweenKey]*changeState{}
		}
		m.changes[key] = c
	case c.id != id:
		c.prev, c.id, c.at = c.id, id, m.now
	}
	c.seen = m.frame
	if c.at.IsZero() {
		return id, 0, false
	}
	return c.prev, a.unscaled(m.now.Sub(c.at)), true
}

// layer is how something is drawn as it moves: faded, moved, scaled about
// its centre and cut back from a side.
type layer struct {
	opacity, scale float64
	dx, dy         float64
	clip           float64 // how much of it is cut away, from 0 to 1
	clipSide       string
}

var restLayer = layer{opacity: 1, scale: 1}

// layerCtx is what an animation's tracks are relative to: the thing's
// rect, for sizes in %, the side it comes from and goes to unless a
// track says, and how far a slide takes it out of sight.
type layerCtx struct {
	rect sdl.FRect
	side string
	auto float64
}

// layerAt is how anim draws something elapsed milliseconds (as written)
// after it began entering or leaving, and whether it is over. Something
// leaving with no motion is over at once.
func (a *App) layerAt(anim config.Animation, elapsed float64, entering bool, ctx layerCtx) (layer, bool) {
	if anim.Duration <= 0 || a.motionScale() <= 0 || len(anim.Tracks) == 0 {
		return restLayer, true
	}
	// Once over, something come in is at rest, and the tracks need not be
	// worked out again, unless one ends elsewhere.
	if entering && elapsed >= anim.Length() && anim.EndsAtRest() {
		return restLayer, true
	}
	l := restLayer
	done := true
	for _, t := range anim.Tracks {
		duration, delay, easing := anim.TrackTiming(t)
		p := 1.0
		if duration > 0 {
			p = (elapsed - delay) / duration
		}
		if p < 1 {
			done = false
		}
		a.applyTrack(&l, t, a.ease(easing, max(0, min(1, p))), entering, ctx)
	}
	if !done {
		a.motion.animating = true
	}
	return l, done
}

// layerEased is how anim draws something at eased progress e through
// entering or leaving, its tracks' own timing aside, as for what lines the
// edges, whose progress is the layout's.
func (a *App) layerEased(anim config.Animation, e float64, entering bool, ctx layerCtx) layer {
	l := restLayer
	for _, t := range anim.Tracks {
		a.applyTrack(&l, t, e, entering, ctx)
	}
	return l
}

// applyTrack sets track t at eased progress e into l.
func (a *App) applyTrack(l *layer, t config.Track, e float64, entering bool, ctx layerCtx) {
	length := func(v float64, size float32) float64 {
		if t.Unit == "%" {
			return v / 100 * float64(size)
		}
		return float64(a.px(v))
	}
	side := t.Side
	if side == "" {
		side = ctx.side
	}
	switch t.Prop {
	case "opacity":
		l.opacity *= max(0, t.At(e, entering))
	case "scale":
		l.scale *= max(0, t.At(e, entering))
	case "x":
		l.dx += length(t.At(e, entering), ctx.rect.W)
	case "y":
		l.dy += length(t.At(e, entering), ctx.rect.H)
	case "clip":
		l.clip, l.clipSide = max(0, min(1, t.At(e, entering))), side
	case "slide":
		var d float64
		size := ctx.rect.H
		if side == "left" || side == "right" {
			size = ctx.rect.W
		}
		if t.Auto {
			out := e // how far out
			if entering {
				out = 1 - e
			}
			d = out * ctx.auto
		} else {
			d = length(t.At(e, entering), size)
		}
		switch side {
		case "top":
			l.dy -= d
		case "left":
			l.dx -= d
		case "right":
			l.dx += d
		default:
			l.dy += d
		}
	}
}

// drawLayer runs draw with what it draws, laid out at rect, drawn as l
// says.
func (a *App) drawLayer(l layer, rect sdl.FRect, draw func() error) error {
	t := a.beginLayer(l, rect)
	if t.skip {
		return nil
	}
	err := draw()
	a.endLayer(&t)
	return err
}

// layerToken is what beginLayer put in place, for endLayer to undo.
type layerToken struct {
	skip, active bool
	fade         float64
	// moved: the viewport before, and how many clips there were.
	moved, hadViewport bool
	viewport           sdl.Rect
	clips              int
	clipped            bool
	// scaled: the texture drawn into, the target before, the region of
	// the window it covers, the viewport's corner, and the scale.
	scaled       bool
	tex, target  *sdl.Texture
	x0, y0, w, h int32
	ox, oy       int32
	rect         sdl.FRect
	k            float32
}

// beginLayer starts drawing what is laid out at rect as l says: moved,
// scaled about its centre, cut back and faded, in that order. What it
// draws until endLayer is drawn so; with skip set, nothing need be.
func (a *App) beginLayer(l layer, rect sdl.FRect) (t layerToken) {
	if l == restLayer {
		return t
	}
	if l.opacity <= 0 || l.scale <= 0 || l.clip >= 1 {
		t.skip = true
		return t
	}
	t.active = true
	m := &a.motion
	if x, y := int32(math.Round(l.dx)), int32(math.Round(l.dy)); x != 0 || y != 0 {
		a.beginOffset(&t, x, y)
	}
	if math.Abs(l.scale-1) > 1e-3 {
		a.beginScale(&t, l.scale, rect)
	}
	if l.clip > 0 {
		// What shows is the part on the side it comes from.
		shown := rect
		keep := float32(1 - l.clip)
		switch l.clipSide {
		case "top":
			shown.H *= keep
		case "left":
			shown.W *= keep
		case "right":
			shown.X += shown.W * (1 - keep)
			shown.W *= keep
		default:
			shown.Y += shown.H * (1 - keep)
			shown.H *= keep
		}
		a.pushClip(a.renderer, shown)
		t.clipped = true
	}
	t.fade = m.fade
	m.fade = 1 - (1-m.fade)*max(0, min(1, l.opacity))
	return t
}

// endLayer undoes what beginLayer put in place.
func (a *App) endLayer(t *layerToken) {
	if !t.active {
		return
	}
	a.motion.fade = t.fade
	if t.clipped {
		a.popClip(a.renderer)
	}
	if t.scaled {
		a.endScale(t)
	}
	if t.moved {
		a.endOffset(t)
	}
}

// setViewport makes r the renderer's viewport, or the whole window.
func (a *App) setViewport(r sdl.Rect, ok bool) {
	m := &a.motion
	m.viewport, m.hasViewport = r, ok
	if a.renderer == nil {
		return
	}
	if ok {
		sdl.SetRenderViewport(a.renderer, &m.viewport)
	} else {
		sdl.SetRenderViewport(a.renderer, nil)
	}
}

// beginOffset moves what is drawn by x, y, by moving the viewport. The
// clip in place is within the viewport; it stays where it is, so what
// moves is cut by what it moves within, as rows by their list.
func (a *App) beginOffset(t *layerToken, x, y int32) {
	m := &a.motion
	t.moved, t.viewport, t.hadViewport, t.clips = true, m.viewport, m.hasViewport, len(a.clips)
	r := sdl.Rect{W: int32(a.winW), H: int32(a.winH)}
	if m.hasViewport {
		r = m.viewport
	}
	r.X += x
	r.Y += y
	a.setViewport(r, true)
	if n := len(a.clips); n > 0 {
		clip := a.clips[n-1]
		clip.X -= x
		clip.Y -= y
		a.clips = append(a.clips, clip)
		if a.renderer != nil {
			sdl.SetRenderClipRect(a.renderer, &a.clips[n])
		}
	}
}

func (a *App) endOffset(t *layerToken) {
	a.setViewport(t.viewport, t.hadViewport)
	if n := t.clips; len(a.clips) > n {
		a.clips = a.clips[:n]
		if a.renderer != nil {
			sdl.SetRenderClipRect(a.renderer, &a.clips[n-1])
		}
	}
}

// layerMargin is how far past a scaled thing's rect its texture reaches,
// in logical pixels, for the shadows drawn round it.
const layerMargin = 64

// beginScale draws what follows into a texture over rect and a margin
// round it, which endScale draws scaled by s about rect's centre.
func (a *App) beginScale(t *layerToken, s float64, rect sdl.FRect) {
	m := &a.motion
	if a.renderer == nil {
		return
	}
	var ox, oy int32
	if m.hasViewport {
		ox, oy = m.viewport.X, m.viewport.Y
	}
	// The part of the window it covers, in the window's pixels.
	margin := a.px(layerMargin)
	x0 := max(0, int32(math.Floor(float64(rect.X-margin)))+ox)
	y0 := max(0, int32(math.Floor(float64(rect.Y-margin)))+oy)
	x1 := min(int32(a.winW), int32(math.Ceil(float64(rect.X+rect.W+margin)))+ox)
	y1 := min(int32(a.winH), int32(math.Ceil(float64(rect.Y+rect.H+margin)))+oy)
	if x1 <= x0 || y1 <= y0 {
		return
	}
	tex := a.layerTexture(m.layerDepth, x1-x0, y1-y0)
	if tex == nil {
		return
	}
	r := a.renderer
	target := sdl.GetRenderTarget(r)
	if !sdl.SetRenderTarget(r, tex) {
		return
	}
	*t = layerToken{skip: t.skip, active: t.active, moved: t.moved, hadViewport: t.hadViewport, viewport: t.viewport, clips: t.clips,
		scaled: true, tex: tex, target: target, x0: x0, y0: y0, w: x1 - x0, h: y1 - y0, ox: ox, oy: oy, rect: rect, k: float32(s)}
	m.layerDepth++
	m.layersUsed = m.frame
	// A texture keeps its own viewport and clip, so the ones in place are
	// set on it, moved so its corner is the region's, and the window's are
	// back once it is done.
	sdl.SetRenderDrawColor(r, 0, 0, 0, 0)
	sdl.RenderClear(r)
	vp := sdl.Rect{X: -x0, Y: -y0, W: int32(a.winW), H: int32(a.winH)}
	if m.hasViewport {
		vp = sdl.Rect{X: m.viewport.X - x0, Y: m.viewport.Y - y0, W: m.viewport.W, H: m.viewport.H}
	}
	sdl.SetRenderViewport(r, &vp)
	if n := len(a.clips); n > 0 {
		sdl.SetRenderClipRect(r, &a.clips[n-1])
	} else {
		sdl.SetRenderClipRect(r, nil)
	}
}

func (a *App) endScale(t *layerToken) {
	r := a.renderer
	a.motion.layerDepth--
	sdl.SetRenderTarget(r, t.target)
	cx, cy, k := t.rect.X+t.rect.W/2, t.rect.Y+t.rect.H/2, t.k
	left, top := float32(t.x0-t.ox), float32(t.y0-t.oy) // the region's corner within the viewport
	src := sdl.FRect{W: float32(t.w), H: float32(t.h)}
	dst := sdl.FRect{X: cx + (left-cx)*k, Y: cy + (top-cy)*k, W: float32(t.w) * k, H: float32(t.h) * k}
	sdl.RenderTexture(r, t.tex, &src, &dst)
}

// layerTexture is a texture at least w by h for scaled layers depth deep
// to draw into, grown as needed up to the window's size.
func (a *App) layerTexture(depth int, w, h int32) *sdl.Texture {
	m := &a.motion
	if a.renderer == nil || depth > 3 {
		return nil
	}
	for len(m.layers) <= depth {
		m.layers = append(m.layers, nil)
	}
	tex := m.layers[depth]
	if tex != nil {
		var tw, th float32
		sdl.GetTextureSize(tex, &tw, &th)
		if int32(tw) >= w && int32(th) >= h {
			return tex
		}
		w, h = max(w, int32(tw)), max(h, int32(th))
		sdl.DestroyTexture(tex)
	}
	tex = sdl.CreateTexture(a.renderer, sdl.PixelFormatRGBA32, sdl.TextureAccessTarget, w, h)
	if tex == nil {
		m.layers[depth] = nil
		return nil
	}
	// What is drawn into it is drawn blended over nothing, so its colours
	// are premultiplied by their alpha.
	sdl.SetTextureBlendMode(tex, sdl.BlendModeBlendPremultiplied)
	sdl.SetTextureScaleMode(tex, sdl.ScaleModeLinear)
	m.layers[depth] = tex
	return tex
}

// releaseLayers frees the textures scaled layers drew into once a frame
// has gone by without one.
func (a *App) releaseLayers() {
	m := &a.motion
	if len(m.layers) == 0 || m.layersUsed+1 >= m.frame {
		return
	}
	for _, tex := range m.layers {
		if tex != nil {
			sdl.DestroyTexture(tex)
		}
	}
	m.layers = nil
}

// faded runs draw with what it draws faded to opacity, within any fade
// already in place.
func (a *App) faded(opacity float64, draw func() error) error {
	m := &a.motion
	old := m.fade
	m.fade = 1 - (1-old)*max(0, min(1, opacity))
	defer func() { m.fade = old }()
	return draw()
}

// changeLayers is how something that changes, now id, is drawn: in, as
// the one now comes in, and, while the one before goes, out, with prev
// its id. A glide moves the one thing instead, whose tracks then play
// in on each change.
func (a *App) changeLayers(key tweenKey, id int, anim config.Animation, ctx layerCtx) (in layer, prev int, out layer, leaving bool) {
	prev, since, ok := a.changed(key, id)
	if !ok || !anim.Moves() {
		return restLayer, id, restLayer, false
	}
	in, _ = a.layerAt(anim, since, true, ctx)
	if anim.Glide || prev == id {
		return in, prev, restLayer, false
	}
	out, done := a.layerAt(anim, since, false, ctx)
	return in, prev, out, !done
}

// viewSide is the side a view comes from and goes to: the side it is
// docked or anchored to, or that it is attached on; one in the middle
// comes from below.
func viewSide(view *uiView) string {
	p := view.place
	switch {
	case p.dock != "":
		return p.dock
	case p.attach != nil:
		switch p.attach.side {
		case "right":
			return "left"
		case "left":
			return "right"
		case "below":
			return "top"
		case "above":
			return "bottom"
		}
	}
	switch p.anchor {
	case "top", "top_left", "top_right":
		return "top"
	case "left":
		return "left"
	case "right":
		return "right"
	}
	return "bottom"
}

// edgeDistance is how far rect must move towards side to leave the
// window.
func (a *App) edgeDistance(rect sdl.FRect, side string) float64 {
	switch side {
	case "top":
		return float64(rect.Y + rect.H)
	case "left":
		return float64(rect.X + rect.W)
	case "right":
		return float64(a.winW) - float64(rect.X)
	}
	return float64(a.winH) - float64(rect.Y)
}

// viewPhase is whether view is entering, rather than leaving, and how
// long it has been.
func (a *App) viewPhase(view *uiView) (entering bool, elapsed float64) {
	if slices.Contains(a.motion.closing, view) {
		return false, a.played(tweenKey{kind: "panel out", view: view})
	}
	return true, a.played(tweenKey{kind: "panel in", view: view})
}

// viewLayer is how view is drawn as it enters or leaves, and whether that
// is over.
func (a *App) viewLayer(view *uiView) (layer, sdl.FRect, bool) {
	entering, elapsed := a.viewPhase(view)
	moment := config.MomentEnter
	if !entering {
		moment = config.MomentExit
	}
	rect, _ := view.frameGeometry(a)
	side := viewSide(view)
	ctx := layerCtx{rect: rect, side: side, auto: a.edgeDistance(rect, side)}
	if view.place.dock != "" {
		ctx.auto = 0 // the edges slide it
	}
	l, done := a.layerAt(a.viewAnim(view, config.MotionPanel, moment), elapsed, entering, ctx)
	return l, rect, done
}

// drawUIViews draws the open views, each coming in as it opens, and those
// just closed going.
func (a *App) drawUIViews(renderer *sdl.Renderer) error {
	m := &a.motion
	view := a.activeUIView()
	if view != m.shownView {
		if view == nil && m.shownView != nil && m.shownView.modal {
			a.startClosing(m.shownView)
		}
		m.shownView = view
	}
	for _, panel := range m.shownPanels {
		if !slices.Contains(a.views.panels, panel) && panel.place.dock == "" {
			a.startClosing(panel)
		}
	}
	m.shownPanels = append(m.shownPanels[:0], a.views.panels...)
	// A view opened again stops closing.
	m.closing = slices.DeleteFunc(m.closing, func(v *uiView) bool { return v.visible })
	// The thumbnails canvases want are those they draw this frame.
	clear(a.canvas.thumbs)
	clear(a.canvas.tiles)
	drawIn := func(v *uiView) error {
		l, rect, _ := a.viewLayer(v)
		draw := func() error { return a.drawLayer(l, rect, func() error { return a.drawUIView(renderer, v) }) }
		if v.place.dock != "" {
			_, dock := a.dockLayout(v)
			inner := draw
			draw = func() error { return a.drawOnEdge(edgeKey{kind: edgePanel, view: v}, dock, inner) }
		}
		return draw()
	}
	// Panels are drawn under the menu. A panel's draw function may close
	// one, so they are drawn from a copy. Menus a menu was stacked over
	// are drawn under it.
	for _, under := range slices.Concat(a.views.panels, a.views.stack) {
		if err := drawIn(under); err != nil {
			return err
		}
	}
	for _, closing := range slices.Clone(m.closing) {
		l, rect, done := a.viewLayer(closing)
		if done {
			m.closing = slices.DeleteFunc(m.closing, func(v *uiView) bool { return v == closing })
			continue
		}
		if err := a.drawLayer(l, rect, func() error { return a.drawUIViewFrame(renderer, closing) }); err != nil {
			return err
		}
	}
	if view == nil {
		return nil
	}
	return drawIn(view)
}

// startClosing draws view leaving from now, once it is closed.
func (a *App) startClosing(view *uiView) {
	m := &a.motion
	if view.visible || slices.Contains(m.closing, view) || !a.viewAnim(view, config.MotionPanel, config.MomentExit).Moves() {
		return
	}
	delete(m.plays, tweenKey{kind: "panel out", view: view})
	m.closing = append(m.closing, view)
}

// forgetViewMotion drops view from what is drawn coming and going, as
// for a view whose plugin is reloaded.
func (a *App) forgetViewMotion(view *uiView) {
	m := &a.motion
	if m.shownView == view {
		m.shownView = nil
	}
	m.shownPanels = slices.DeleteFunc(m.shownPanels, func(v *uiView) bool { return v == view })
	m.closing = slices.DeleteFunc(m.closing, func(v *uiView) bool { return v == view })
}
