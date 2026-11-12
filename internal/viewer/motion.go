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
	// regionX, regionY is the window corner of the scaled layer's texture
	// being drawn into, which the viewport is set on less that corner.
	regionX, regionY int32
	layers           []*sdl.Texture
	layerDepth       int
	// scales are the scaled layers being drawn into, outermost first:
	// the point each is scaled about, in the window's pixels, and by how
	// much.
	scales     []layerScale
	layersUsed uint64      // the last frame a scaled layer was drawn
	osReduced  atomic.Bool // the OS asks for less motion; see reducedMotion
	// osReading is whether the OS's setting is being read, and osReadAt
	// when it was last read; see refreshReducedMotion.
	osReading atomic.Bool
	osReadAt  time.Time
	// documentSwitch is the document of the tab shown before, going.
	documentSwitch *documentSwitch
	// deferBorders is set while a panel is drawn into a scaled layer, whose
	// borders, a pixel wide, would come out of it soft and faint; they are
	// kept in borders and drawn sharp over the layer, as scaled, instead.
	deferBorders bool
	borders      []deferredBorder
}

// deferredBorder is a panel's border to draw over its scaled layer.
type deferredBorder struct {
	st   config.Style
	rect sdl.FRect
}

// tweenKey names something that moves: what it is, and the view or text
// it belongs to, if any.
type tweenKey struct {
	kind string
	view *uiView
	text string
}

// tween is a value moving to its target. Each time the target moves, a
// segment eases the distance from the old target to the new away to
// nothing, while those begun before play on: the value is the target
// plus what each segment has left. So retargeted mid-way it moves on
// from where it is, at the pace it had, never jumping, and under a key
// held faster than a transition runs it trails by at most the steps of
// one transition.
type tween struct {
	to, value float64
	segments  []tweenSegment
	// began is the frame the target last moved, and startValue where the
	// value was then.
	began      time.Time
	startValue float64
	seen       uint64 // the last frame the value was asked for
}

// tweenSegment is what is left of one move of a tween's target: it eases
// from delta, the old target less the new, to nothing.
type tweenSegment struct {
	delta float64
	start time.Time
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

// viewAnim is anim with view's own motion over it. A view's change moves
// its keys too while neither it nor the theme says how keys move.
func (a *App) viewAnim(view *uiView, target config.MotionTarget, moment config.Moment) config.Animation {
	anim := a.anim(target, moment)
	if view != nil && view.motion != nil {
		own := view.motion[target][moment]
		if moment == config.MomentKeys && own == nil && !a.config.Theme.Motion.KeysSet[target] {
			own = view.motion[target][config.MomentChange]
		}
		anim = own.Apply(anim)
	}
	return anim
}

// pressLayer is how something held down is drawn: anim's tracks going out
// as it is pressed and coming back as it is let go, as far as key's
// tween, which retargets smoothly on a quick tap, has it.
func (a *App) pressLayer(key tweenKey, pressed bool, anim config.Animation, ctx layerCtx) layer {
	if len(anim.Tracks) == 0 {
		return restLayer
	}
	target := 0.0
	if pressed {
		target = 1
	}
	p := a.animate(key, target, anim)
	if p == 0 {
		return restLayer
	}
	l := restLayer
	for _, t := range anim.Tracks {
		a.applyTrack(&l, t, p, false, ctx)
	}
	return l
}

// pressShows reports whether target has a press to show, so that what is
// pressed is chosen as it is let go rather than as it is pressed.
func (a *App) pressShows(view *uiView, target config.MotionTarget) bool {
	return a.motionScale() > 0 && a.viewAnim(view, target, config.MomentPress).Moves()
}

// selectionMoment is the moment a selection's move is: change when the
// pointer made it, keys when anything else did.
func selectionMoment(byPointer bool) config.Moment {
	if byPointer {
		return config.MomentChange
	}
	return config.MomentKeys
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

// glideSpan is the span lo to hi on its way to where it goes, as anim
// glides it, each end gliding on its own, from fromLo and fromHi when it
// was not drawn last frame (NaN for where it is). The end it heads
// towards leads; with a stretch, the other sets off that much later, so
// the span stretches as it goes and, under a spring, wobbles as it
// settles. It also returns how much longer than at rest the span is, as
// a factor, for what crosses it to squeeze by.
func (a *App) glideSpan(key tweenKey, lo, hi, fromLo, fromHi float64, anim config.Animation) (float64, float64, float64) {
	if !anim.Glide {
		anim.Duration = 0
	}
	loKey, hiKey, lenKey := key, key, key
	loKey.kind += " lo"
	hiKey.kind += " hi"
	lenKey.kind += " length"
	if anim.Stretch <= 0 || anim.Duration <= 0 {
		return a.animateFrom(loKey, fromLo, lo, anim), a.animateFrom(hiKey, fromHi, hi, anim), 1
	}
	// Which way it heads: towards its new place, or the way it was going.
	dir := 0.0
	m := &a.motion
	if tw := m.tweens[loKey]; tw != nil && tw.seen+1 >= m.frame {
		if tw.to != lo {
			dir = lo - tw.to
		} else if n := len(tw.segments); n > 0 {
			dir = -tw.segments[n-1].delta
		}
	} else if !math.IsNaN(fromLo) {
		dir = lo - fromLo
	}
	lead, trail := anim, anim
	trail.Delay += anim.Stretch
	loAnim, hiAnim := trail, lead // heading up, hi leads
	if dir < 0 {
		loAnim, hiAnim = lead, trail
	}
	gLo := a.animateFrom(loKey, fromLo, lo, loAnim)
	gHi := a.animateFrom(hiKey, fromHi, hi, hiAnim)
	fromLength := math.NaN()
	if !math.IsNaN(fromLo) && !math.IsNaN(fromHi) {
		fromLength = fromHi - fromLo
	}
	length := a.animateFrom(lenKey, fromLength, hi-lo, lead)
	stretch := 1.0
	if length > 0 && gHi > gLo {
		stretch = (gHi - gLo) / length
	}
	return gLo, gHi, stretch
}

// glideLength is glide for a length, with how far from its rest shape the
// thing it measures is as it changes, as a factor of its length: with a
// stretch, how much longer it is than it would be that much later, so a
// thing growing fast thins, and one a spring pulls back swells, before it
// settles. The factor stays within a third of rest either way.
func (a *App) glideLength(key tweenKey, target float64, anim config.Animation) (float64, float64) {
	v := a.glide(key, target, anim)
	if anim.Stretch <= 0 || !anim.Glide || anim.Duration <= 0 {
		return v, 1
	}
	lag := anim
	lag.Delay += anim.Stretch
	key.kind += " lag"
	behind := a.glide(key, target, lag)
	size := max(math.Abs(v), math.Abs(behind), math.Abs(target))
	if size <= 0 {
		return v, 1
	}
	return v, 1 + max(-1.0/3, min(1.0/3, (v-behind)/size))
}

// squeeze is the span lo to hi narrowed about its middle as a liquid is
// when stretched the other way by stretch, keeping about its area; a
// span squashed, stretch under 1, widens.
func squeeze(lo, hi, stretch float64) (float64, float64) {
	if stretch == 1 || stretch <= 0 {
		return lo, hi
	}
	mid, half := (lo+hi)/2, (hi-lo)/2/math.Sqrt(stretch)
	return mid - half, mid + half
}

// glideRect is r on its way to where it goes, as anim glides it, each
// side gliding on its own as glideSpan has them, from from if r was not
// drawn last frame and from is given; it squeezes across as it stretches
// along.
func (a *App) glideRect(key tweenKey, r sdl.FRect, from *sdl.FRect, anim config.Animation) sdl.FRect {
	fx0, fx1, fy0, fy1 := math.NaN(), math.NaN(), math.NaN(), math.NaN()
	if from != nil {
		fx0, fx1, fy0, fy1 = float64(from.X), float64(from.X+from.W), float64(from.Y), float64(from.Y+from.H)
	}
	kx, ky := key, key
	kx.kind += " x"
	ky.kind += " y"
	x0, x1, sx := a.glideSpan(kx, float64(r.X), float64(r.X+r.W), fx0, fx1, anim)
	y0, y1, sy := a.glideSpan(ky, float64(r.Y), float64(r.Y+r.H), fy0, fy1, anim)
	x0, x1 = squeeze(x0, x1, sy)
	y0, y1 = squeeze(y0, y1, sx)
	return sdl.FRect{X: float32(x0), Y: float32(y0), W: float32(max(0, x1-x0)), H: float32(max(0, y1-y0))}
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
		tw = &tween{to: target, value: from, began: m.now, startValue: from}
		if from != target {
			tw.segments = []tweenSegment{{delta: from - target, start: m.now}}
		}
		if m.tweens == nil {
			m.tweens = map[tweenKey]*tween{}
		}
		m.tweens[key] = tw
	case tw.to != target:
		// A new target moves on from where the value is, the segments
		// begun before playing on.
		tw.segments = append(tw.segments, tweenSegment{delta: tw.to - target, start: m.now})
		tw.to, tw.began, tw.startValue = target, m.now, tw.value
	}
	tw.seen = m.frame
	duration := a.scaledMs(anim.Duration)
	tw.value = tw.to
	live := tw.segments[:0]
	for _, seg := range tw.segments {
		progress := 1.0
		if duration > 0 {
			progress = float64(m.now.Sub(seg.start)-a.scaledMs(anim.Delay)) / float64(duration)
		}
		if progress >= 1 || seg.delta == 0 {
			continue
		}
		tw.value += seg.delta * (1 - a.ease(anim.Easing, max(0, progress)))
		live = append(live, seg)
	}
	clear(tw.segments[len(live):])
	tw.segments = live
	if len(live) > 0 {
		m.animating = true
	}
	return tw.value
}

// tweenBegan reports whether key's target moved this frame, and where its
// value was then.
func (a *App) tweenBegan(key tweenKey) (float64, bool) {
	tw := a.motion.tweens[key]
	if tw == nil || !tw.began.Equal(a.motion.now) {
		return 0, false
	}
	return tw.startValue, true
}

// shiftTween moves where key's latest move starts by d, if it began this
// frame, and returns where its value is now.
func (a *App) shiftTween(key tweenKey, d float64) float64 {
	tw := a.motion.tweens[key]
	if tw == nil {
		return math.NaN()
	}
	if n := len(tw.segments); d != 0 && n > 0 && tw.began.Equal(a.motion.now) && tw.segments[n-1].start.Equal(a.motion.now) {
		tw.segments[n-1].delta += d
		tw.value += d
		tw.startValue += d
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
	// squashX and squashY scale it further across and down, as a bubble
	// squashes, as so much more than 1: 0 for none.
	squashX, squashY float64
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
	if entering && elapsed >= anim.Length()+anim.Stretch && anim.EndsAtRest() {
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
	// With a stretch, what grows or shrinks does so across first and down
	// that much later, as a bubble does: wide and flat as it pops open,
	// then rounding out.
	if anim.Stretch > 0 && l.scale > 0 {
		behind := restLayer
		for _, t := range anim.Tracks {
			if t.Prop != "scale" {
				continue
			}
			duration, delay, easing := anim.TrackTiming(t)
			p := 1.0
			if duration > 0 {
				p = (elapsed - delay - anim.Stretch) / duration
			}
			if p < 1 {
				done = false
			}
			a.applyTrack(&behind, t, a.ease(easing, max(0, min(1, p))), entering, ctx)
		}
		l.squashY = behind.scale/l.scale - 1
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
	kx, ky       float32
	// regionX, regionY: the corner of the texture drawn into before.
	regionX, regionY int32
}

// beginLayer starts drawing what is laid out at rect as l says: moved,
// scaled about its centre, cut back and faded, in that order. What it
// draws until endLayer is drawn so; with skip set, nothing need be.
func (a *App) beginLayer(l layer, rect sdl.FRect) (t layerToken) {
	if l == restLayer {
		return t
	}
	kx, ky := l.scale*(1+l.squashX), l.scale*(1+l.squashY)
	if l.opacity <= 0 || kx <= 0 || ky <= 0 || l.clip >= 1 {
		t.skip = true
		return t
	}
	t.active = true
	m := &a.motion
	if x, y := int32(math.Round(l.dx)), int32(math.Round(l.dy)); x != 0 || y != 0 {
		a.beginOffset(&t, x, y)
	}
	// A scale that changes its size by under half a pixel is none: drawn
	// scaled it would be resampled, a little soft, and then sharpen as it
	// settled.
	if math.Abs(kx-1)*float64(rect.W) >= 0.5 || math.Abs(ky-1)*float64(rect.H) >= 0.5 {
		a.beginScaleXY(&t, kx, ky, rect)
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

// setViewport makes r, in the window, the renderer's viewport, or the
// whole window. Within a scaled layer it is set on the layer's texture,
// moved by the texture's corner.
func (a *App) setViewport(r sdl.Rect, ok bool) {
	m := &a.motion
	m.viewport, m.hasViewport = r, ok
	if a.renderer == nil {
		return
	}
	if !ok && m.regionX == 0 && m.regionY == 0 {
		sdl.SetRenderViewport(a.renderer, nil)
		return
	}
	if !ok {
		r = sdl.Rect{W: int32(a.winW), H: int32(a.winH)}
	}
	r.X -= m.regionX
	r.Y -= m.regionY
	sdl.SetRenderViewport(a.renderer, &r)
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
		setClip(a.renderer, &a.clips[n])
	}
}

func (a *App) endOffset(t *layerToken) {
	a.setViewport(t.viewport, t.hadViewport)
	if n := t.clips; len(a.clips) > n {
		a.clips = a.clips[:n]
		setClip(a.renderer, &a.clips[n-1])
	}
}

// layerScale is a layer shown scaled by kx across and ky down about x, y,
// drawn into tex, whose corner is x0, y0 in the window.
type layerScale struct {
	x, y, kx, ky float32
	tex          *sdl.Texture
	x0, y0       int32
}

// layerMargin is how far past a scaled thing's rect its texture reaches,
// in logical pixels, for the shadows drawn round it: as far as a deep
// shadow's blur and offset go, so none is cut off while it moves and then
// shown whole as it settles.
const layerMargin = 128

// beginScale draws what follows into a texture over rect and a margin
// round it, which endScale draws scaled by s about rect's centre.
func (a *App) beginScale(t *layerToken, s float64, rect sdl.FRect) {
	a.beginScaleXY(t, s, s, rect)
}

// beginScaleXY is beginScale scaling by kx across and ky down.
func (a *App) beginScaleXY(t *layerToken, kx, ky float64, rect sdl.FRect) {
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
		scaled: true, tex: tex, target: target, x0: x0, y0: y0, w: x1 - x0, h: y1 - y0, ox: ox, oy: oy, rect: rect, kx: float32(kx), ky: float32(ky),
		regionX: m.regionX, regionY: m.regionY}
	m.layerDepth++
	m.layersUsed = m.frame
	m.scales = append(m.scales, layerScale{x: rect.X + rect.W/2 + float32(ox), y: rect.Y + rect.H/2 + float32(oy), kx: float32(kx), ky: float32(ky), tex: tex, x0: x0, y0: y0})
	// A texture keeps its own viewport and clip, so the ones in place are
	// set on it, moved so its corner is the region's, and the window's are
	// back once it is done. Viewports set while drawing into it are moved
	// the same way; see setViewport.
	sdl.SetRenderDrawColor(r, 0, 0, 0, 0)
	sdl.RenderClear(r)
	m.regionX, m.regionY = x0, y0
	a.setViewport(m.viewport, m.hasViewport)
	if n := len(a.clips); n > 0 {
		setClip(r, &a.clips[n-1])
	} else {
		setClip(r, nil)
	}
}

func (a *App) endScale(t *layerToken) {
	r := a.renderer
	m := &a.motion
	m.layerDepth--
	m.scales = m.scales[:len(m.scales)-1]
	m.regionX, m.regionY = t.regionX, t.regionY
	sdl.SetRenderTarget(r, t.target)
	cx, cy, kx, ky := t.rect.X+t.rect.W/2, t.rect.Y+t.rect.H/2, t.kx, t.ky
	left, top := float32(t.x0-t.ox), float32(t.y0-t.oy) // the region's corner within the viewport
	src := sdl.FRect{W: float32(t.w), H: float32(t.h)}
	dst := sdl.FRect{X: cx + (left-cx)*kx, Y: cy + (top-cy)*ky, W: float32(t.w) * kx, H: float32(t.h) * ky}
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
		draw := func() error {
			return a.drawScaledPanel(renderer, l, rect, func() error { return a.drawUIView(renderer, v) })
		}
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
		if err := a.drawScaledPanel(renderer, l, rect, func() error { return a.drawUIViewFrame(renderer, closing) }); err != nil {
			return err
		}
	}
	if view == nil {
		return nil
	}
	return drawIn(view)
}

// drawScaledPanel is drawLayer for a view's panel: while l scales it, the
// panels' borders are drawn over the layer, sharp round where it has
// scaled them to, rather than scaled with it, soft and faint until it
// settles and they are drawn whole.
func (a *App) drawScaledPanel(renderer *sdl.Renderer, l layer, rect sdl.FRect, draw func() error) error {
	m := &a.motion
	kx, ky := l.scale*(1+l.squashX), l.scale*(1+l.squashY)
	scaled := math.Abs(kx-1)*float64(rect.W) >= 0.5 || math.Abs(ky-1)*float64(rect.H) >= 0.5
	if !scaled || l.clip > 0 || l.opacity <= 0 || kx <= 0 || ky <= 0 || m.deferBorders {
		return a.drawLayer(l, rect, draw)
	}
	m.deferBorders, m.borders = true, m.borders[:0]
	err := a.drawLayer(l, rect, draw)
	m.deferBorders = false
	cx, cy := rect.X+rect.W/2, rect.Y+rect.H/2
	k := min(kx, ky)
	for _, b := range m.borders {
		r := sdl.FRect{
			X: cx + (b.rect.X-cx)*float32(kx) + float32(l.dx), Y: cy + (b.rect.Y-cy)*float32(ky) + float32(l.dy),
			W: b.rect.W * float32(kx), H: b.rect.H * float32(ky),
		}
		st := b.st
		for i := range st.Radius.V {
			st.Radius.V[i] *= k
		}
		_ = a.faded(l.opacity, func() error { a.drawBoxParts(renderer, &st, r, boxBorder); return nil })
	}
	m.borders = m.borders[:0]
	return err
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
