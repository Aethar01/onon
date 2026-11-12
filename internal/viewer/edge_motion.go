package viewer

import (
	"cmp"
	"math"
	"slices"
	"time"

	"onon/internal/config"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// What lines the window's edges, the bars and docked views, slides in and
// out as motion.edges says, taking the room it needs from the document as
// it goes: each frame the document is laid out for what they leave it, as
// smooth zoom lays it out for each step, so the page moves only as it
// must, with them, and a fit mode is kept all the way.

// edgeKey names what lines an edge.
type edgeKey struct {
	kind edgeKind
	view *uiView // a docked view
}

func (item edgeItem) key() edgeKey { return edgeKey{kind: item.kind, view: item.view} }

// edgeSlide is how far in something on an edge is as it moves, from 0 to
// 1, going from from to to.
type edgeSlide struct {
	from, to float64
	// item is what slides; a bar hidden is laid out and drawn while it
	// slides out. A docked view closed is gone, and goes at once.
	item edgeItem
}

// edgeMotion is a move along the edges in progress.
type edgeMotion struct {
	active bool
	start  time.Time
	anim   config.Animation // the timing of the move: edges' enter if anything comes in, else its exit
	slides map[edgeKey]*edgeSlide
	e      float64 // how far the move has come this frame
}

// edgeSnapshot is what lined the edges before a change to them.
type edgeSnapshot struct {
	ok    bool
	now   time.Time
	items []edgeItem
}

// edgeDuration is how long a move along the edges may take, in ms: the
// longer of coming in and going, 0 if neither moves.
func (a *App) edgeDuration() float64 {
	enter, exit := a.anim(config.MotionEdges, config.MomentEnter), a.anim(config.MotionEdges, config.MomentExit)
	return max(enter.Duration, exit.Duration) * a.motionScale()
}

// edgeProgress is how far the move has come at now, eased: 1 once over.
func (a *App) edgeProgress(now time.Time) float64 {
	m := &a.edgeMotion
	duration := m.anim.Duration * a.motionScale()
	if !m.active || duration <= 0 {
		return 1
	}
	p := float64(now.Sub(m.start)) / (duration * float64(time.Millisecond))
	if p >= 1 {
		return 1
	}
	return a.ease(m.anim.Easing, max(0, p))
}

// value is how far in s is at progress e.
func (s *edgeSlide) value(e float64) float64 { return s.from + (s.to-s.from)*e }

// snapshotEdges records the edges before what changes them, unless they
// would not move: with motion.edges off, or before the window is drawn.
func (a *App) snapshotEdges() edgeSnapshot {
	if a.edgeDuration() <= 0 || a.motion.frame == 0 || a.background {
		return edgeSnapshot{}
	}
	return edgeSnapshot{ok: true, now: time.Now(), items: a.edgeItems()}
}

// edgesChanged starts a move from before, if what lines the edges has
// changed since: a bar or docked view shown or hidden, not one resized.
// Everything starts where it was, so the layout that follows it is the
// one before.
func (a *App) edgesChanged(before edgeSnapshot) {
	if !before.ok {
		return
	}
	after := a.edgeItems()
	if sameEdgeKeys(before.items, after) {
		return
	}
	m := &a.edgeMotion
	e := a.edgeProgress(before.now)
	anim := a.anim(config.MotionEdges, config.MomentExit)
	for _, item := range after {
		if !hasEdgeKey(before.items, item.key()) {
			anim = a.anim(config.MotionEdges, config.MomentEnter)
		}
	}
	// Each slides on from where it is, so a move turned round part way
	// goes back without a jump.
	current := func(key edgeKey, shown bool) float64 {
		if s := m.slides[key]; m.active && s != nil {
			return s.value(e)
		}
		if shown {
			return 1
		}
		return 0
	}
	slides := make(map[edgeKey]*edgeSlide)
	for _, item := range after {
		slides[item.key()] = &edgeSlide{from: current(item.key(), hasEdgeKey(before.items, item.key())), to: 1, item: item}
	}
	for _, item := range before.items {
		if hasEdgeKey(after, item.key()) || item.kind == edgePanel {
			continue
		}
		slides[item.key()] = &edgeSlide{from: current(item.key(), true), to: 0, item: item}
	}
	if m.active { // bars still sliding out from the move before
		for key, s := range m.slides {
			if _, ok := slides[key]; !ok && s.to == 0 {
				slides[key] = &edgeSlide{from: s.value(e), to: 0, item: s.item}
			}
		}
	}
	m.slides, m.start, m.active, m.e, m.anim = slides, before.now, true, 0, anim
	a.pendingRedraw = true
}

// stepEdgeMotion moves the edges on to this frame, at its start, laying
// the document out for the room they leave it.
func (a *App) stepEdgeMotion() {
	m := &a.edgeMotion
	if !m.active {
		return
	}
	// The step is the layout's update, so the page keeps its place from
	// where it was the frame before.
	a.relayoutWithViewportAnchor(func() {
		m.e = a.edgeProgress(a.motion.now)
		// A stretch keeps the move going until its trailing edge is in too.
		if m.e < 1 || a.edgeLag() < 1 {
			a.motion.animating = true
		} else {
			m.active, m.slides = false, nil
		}
		a.views.layoutGen++
	})
}

// edgeItemsMoving is what is laid out along the edges: what lines them,
// and the bars still sliding out.
func (a *App) edgeItemsMoving() []edgeItem {
	items := a.edgeItems()
	m := &a.edgeMotion
	if !m.active {
		return items
	}
	for _, s := range m.slides {
		if s.to == 0 {
			items = append(items, s.item)
		}
	}
	slices.SortStableFunc(items, func(x, y edgeItem) int { return cmp.Compare(x.order, y.order) })
	return items
}

// edgeIn is how far in item is, 1 once at rest.
func (a *App) edgeIn(item edgeItem) float64 {
	m := &a.edgeMotion
	if s := m.slides[item.key()]; m.active && s != nil {
		return s.value(m.e)
	}
	return 1
}

// edgeGhost reports whether a bar of kind is sliding out.
func (a *App) edgeGhost(kind edgeKind) bool {
	m := &a.edgeMotion
	s := m.slides[edgeKey{kind: kind}]
	return m.active && s != nil && s.to == 0
}

// drawOnEdge draws what lines an edge, laid out at r, as far in as it is:
// with edges' tracks, a slide out of the window by default, at how far it
// has come in or gone out.
func (a *App) drawOnEdge(key edgeKey, r sdl.FRect, draw func() error) error {
	m := &a.edgeMotion
	s := m.slides[key]
	if !m.active || s == nil {
		return draw()
	}
	in := s.value(m.e)
	entering := s.to > s.from
	moment, e := config.MomentEnter, in
	if !entering {
		moment, e = config.MomentExit, 1-in
	}
	auto := float64(r.H)
	if s.item.side == "left" || s.item.side == "right" {
		auto = float64(r.W)
	}
	anim := a.anim(config.MotionEdges, moment)
	l := a.layerEased(anim, e, entering, layerCtx{rect: r, side: s.item.side, auto: auto})
	// With a stretch it stretches the way it goes as it slides, as far as
	// it is ahead of where it would be that much later, and thins across,
	// swelling as a spring brings it back: gently, as the page moves too.
	if anim.Stretch > 0 {
		ahead := in - s.value(a.edgeLag())
		if !entering {
			ahead = -ahead
		}
		along := max(-0.12, min(0.12, ahead*0.5))
		across := 1/math.Sqrt(1+along) - 1
		if s.item.side == "left" || s.item.side == "right" {
			l.squashX, l.squashY = along, across
		} else {
			l.squashX, l.squashY = across, along
		}
	}
	return a.drawLayer(l, r, draw)
}

// edgeLag is how far the move along the edges had come its stretch
// before now, for what lines the edges to stretch by as it goes.
func (a *App) edgeLag() float64 {
	stretch := a.edgeMotion.anim.Stretch
	if stretch <= 0 {
		return a.edgeMotion.e
	}
	return a.edgeProgress(a.motion.now.Add(-a.scaledMs(stretch)))
}

func hasEdgeKey(items []edgeItem, key edgeKey) bool {
	for _, item := range items {
		if item.key() == key {
			return true
		}
	}
	return false
}

func sameEdgeKeys(a, b []edgeItem) bool {
	if len(a) != len(b) {
		return false
	}
	for _, item := range a {
		if !hasEdgeKey(b, item.key()) {
			return false
		}
	}
	return true
}
