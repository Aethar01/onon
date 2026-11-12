package config

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

// Motion is how the UI moves. Each thing that moves, a target, has an
// animation for each moment it moves at: entering, leaving, or changing,
// as the selection does when it moves to another row. An animation is a
// timing and a set of tracks, the properties it moves, such as opacity
// and offset. Scale slows or speeds them all.
//
// Reduced is the motion used instead when the OS asks for less motion, or
// ForceReduced is set: by default the same with only fades left.
type Motion struct {
	Scale        float64
	ForceReduced bool
	Moves        Moves
	Reduced      ReducedMotion
	// KeysSet is whether a target's keys moment was given: until it is,
	// a target moves as its change moment says whatever moved it.
	KeysSet [MotionTargetCount]bool
}

// Moves are the animations of each target at each moment.
type Moves [MotionTargetCount][MomentCount]Animation

// ReducedMotion is the motion used while motion is reduced: Base, a
// version of the motion, with Patches over it.
type ReducedMotion struct {
	Base    string // fade (the motion with only fades left), none or full
	Scale   Opt[float64]
	Patches [MotionTargetCount][MomentCount]*AnimationPatch
}

// MotionTarget is something that moves.
type MotionTarget int

const (
	MotionPanel MotionTarget = iota
	MotionRows
	MotionSelection
	MotionOverviewSelection
	MotionPrompt
	MotionMessage
	MotionCursor
	MotionEdges
	MotionTitleBar
	MotionTabSelection
	MotionHover
	MotionDocument
	MotionTargetCount
)

// Moment is when a target moves.
type Moment int

const (
	MomentEnter Moment = iota
	MomentExit
	MomentChange
	// MomentKeys is a change made other than by the pointer, as by the
	// keys; as MomentChange unless set.
	MomentKeys
	// MomentPress is the selection held down, by Enter or the pointer, its
	// tracks going out as it is pressed and coming back as it is let go.
	MomentPress
	MomentCount
)

var momentNames = [MomentCount]string{"enter", "exit", "change", "keys", "press"}

func (m Moment) String() string { return momentNames[m] }

// motionTargetInfo describes a target: its name in the motion table, what
// it is, the moments an animation set for the whole target is for, and
// those it also moves at that are set only by name.
type motionTargetInfo struct {
	name, description string
	moments           []Moment
	named             []Moment
}

var enterExit, change = []Moment{MomentEnter, MomentExit}, []Moment{MomentChange}

var motionTargets = [MotionTargetCount]motionTargetInfo{
	MotionPanel:             {"panel", "Menus, panels and completion opening and closing. A slide comes in from the side the view is anchored to. A view given another size glides to it in the time it takes to open.", enterExit, nil},
	MotionRows:              {"rows", "A list's rows as its view opens and closes, one after another with stagger.", enterExit, nil},
	MotionSelection:         {"selection", "The selected row's highlight moving to another row: glide moves it there, other tracks bring the new one in and take the old one out. keys, set by name, is its move when the pointer did not make it, as change unless set; press, its tracks as Enter or the pointer holds it down, and the row is then chosen as they are let go.", change, []Moment{MomentKeys, MomentPress}},
	MotionOverviewSelection: {"overview_selection", "The outline round the overview's selected page moving to another, as selection does, keys too.", change, []Moment{MomentKeys}},
	MotionPrompt:            {"prompt", "The sides of the status bar growing and shrinking with their text, as a pill does when a prompt opens; only its timing and glide count.", change, nil},
	MotionMessage:           {"message", "Status messages coming in, and going once cleared.", enterExit, nil},
	MotionCursor:            {"cursor", "The text cursor moving, as selection does.", change, nil},
	MotionEdges:             {"edges", "The status bar, tab bar and docked views coming in along the window's edges as they are shown and going as they are hidden, the page moving with them only where it must. The timing moves the page; the tracks follow it, a slide by default.", enterExit, nil},
	MotionTitleBar:          {"title_bar", "The window's buttons over the page appearing as the pointer reaches the top of the window.", enterExit, nil},
	MotionTabSelection:      {"tab_selection", "The shown tab's highlight moving to the tab shown next, as selection does: glide moves it there, each tab's own box clearing as it passes over, and other tracks bring the new one in and take the old one out. press, set by name, is a tab held down by the pointer, shown as it is let go.", change, []Moment{MomentPress}},
	MotionHover:             {"hover", "A tab taking its hovered look as the pointer comes onto it, and losing it as the pointer goes; only its timing counts.", enterExit, nil},
	MotionDocument:          {"document", "The document going as another tab's is shown, the new one coming in under it: its tracks move the one before out, towards the start of the bar when the tab shown next is after it, and the new one in from the other side; fades only the one before.", change, nil},
}

func (t MotionTarget) String() string { return motionTargets[t].name }

// Moments are the moments an animation set for the whole of t is for.
func (t MotionTarget) Moments() []Moment { return motionTargets[t].moments }

// AllMoments are all the moments t moves at, those set only by name too.
func (t MotionTarget) AllMoments() []Moment {
	return slices.Concat(motionTargets[t].moments, motionTargets[t].named)
}

// MotionTargetNamed returns the target of that name.
func MotionTargetNamed(name string) (MotionTarget, bool) {
	for t, info := range motionTargets {
		if info.name == name {
			return MotionTarget(t), true
		}
	}
	return 0, false
}

// Animation is how something moves at one moment: for how long, after
// what delay, eased how, and which tracks. Stagger delays each of several
// things, such as rows, by that much more than the one before. Glide, for
// a change, moves the thing itself to where it goes; without it the
// tracks bring the new one in and take the old one out. Stretch, for a
// glide, sets the trailing edge off that much after the leading one, so
// the thing stretches as it goes and, under a spring, wobbles as it
// settles.
type Animation struct {
	Duration float64 // milliseconds; 0 for none
	Delay    float64
	Stagger  float64
	Stretch  float64
	Easing   Easing
	Glide    bool
	Tracks   []Track
}

// Track is one property an animation moves. Written as one value, From
// is where the property is while the thing is hidden: it comes from there
// as it enters and goes there as it leaves. Written as two, Pair, it
// moves from From to To either way.
type Track struct {
	Prop     string // opacity, x, y, scale, slide or clip
	From, To float64
	Pair     bool
	Unit     string // for x, y and slide: px, or % of the thing's own size
	Auto     bool   // for slide: as far as takes the thing out of the window
	Side     string // for slide and clip: the side it goes to; empty for the thing's own
	Timing   *TrackTiming
}

// TrackTiming is a track's own timing, over its animation's.
type TrackTiming struct {
	Duration, Delay Opt[float64]
	Easing          *Easing
}

// trackProps are the properties a track can move, with their values at
// rest.
var trackProps = map[string]float64{"opacity": 1, "x": 0, "y": 0, "scale": 1, "slide": 0, "clip": 0}

// trackOrder is the order tracks are written in.
var trackOrder = []string{"opacity", "x", "y", "scale", "slide", "clip"}

// Rest is the track's value at rest.
func (t Track) Rest() float64 { return trackProps[t.Prop] }

// At is the track's value at eased progress e, entering or leaving.
func (t Track) At(e float64, entering bool) float64 {
	if t.Pair {
		return t.From + (t.To-t.From)*e
	}
	if entering {
		return t.From + (t.Rest()-t.From)*e
	}
	return t.Rest() + (t.From-t.Rest())*e
}

// Easing is how a change speeds and slows: a cubic-bezier curve, steps, a
// spring, or a Lua function of the time through it.
type Easing struct {
	Name   string // as written
	Curve  [4]float64
	Steps  int
	Spring [2]float64 // stiffness and damping
	Func   *lua.LFunction
}

// easings are the named easings, as cubic-bezier control points.
var easings = map[string][4]float64{
	"linear":      {0, 0, 1, 1},
	"ease":        {0.25, 0.1, 0.25, 1},
	"ease-in":     {0.42, 0, 1, 1},
	"ease-out":    {0, 0, 0.58, 1},
	"ease-in-out": {0.42, 0, 0.58, 1},
}

func namedEasing(name string) Easing { return Easing{Name: name, Curve: easings[name]} }

// Ease is how far through its change an easing is at fraction p of its
// time. A Lua function is the caller's to call; Ease gives p for it.
func (e Easing) Ease(p float64) float64 {
	if p <= 0 {
		return 0
	}
	if p >= 1 {
		return 1
	}
	switch {
	case e.Func != nil || e.Name == "":
		return p
	case e.Steps > 0:
		return math.Floor(p*float64(e.Steps)) / float64(e.Steps)
	case e.Spring[0] > 0:
		return springAt(e.Spring[0], e.Spring[1], p)
	}
	x1, y1, x2, y2 := e.Curve[0], e.Curve[1], e.Curve[2], e.Curve[3]
	bezier := func(a, b, s float64) float64 { // one coordinate of the curve, from 0 to 1
		return 3*a*s*(1-s)*(1-s) + 3*b*s*s*(1-s) + s*s*s
	}
	// Find the curve's parameter where x is p; x rises with it, as x1 and
	// x2 lie from 0 to 1, so halving the interval converges.
	lo, hi := 0.0, 1.0
	for range 30 {
		mid := (lo + hi) / 2
		if bezier(x1, x2, mid) < p {
			lo = mid
		} else {
			hi = mid
		}
	}
	return bezier(y1, y2, (lo+hi)/2)
}

// springAt is where a spring of unit mass let go from 0 towards 1 is at
// fraction p of the time it takes to settle.
func springAt(stiffness, damping, p float64) float64 {
	w := math.Sqrt(stiffness)
	zeta := damping / (2 * w)
	var decay float64 // the slowest rate it settles at
	switch {
	case zeta < 1:
		decay = zeta * w
	case zeta == 1:
		decay = w
	default:
		decay = w * (zeta - math.Sqrt(zeta*zeta-1))
	}
	t := p * math.Log(1000) / decay // settled to a thousandth
	switch {
	case zeta < 1:
		wd := w * math.Sqrt(1-zeta*zeta)
		return 1 - math.Exp(-zeta*w*t)*(math.Cos(wd*t)+zeta*w/wd*math.Sin(wd*t))
	case zeta == 1:
		return 1 - math.Exp(-w*t)*(1+w*t)
	}
	s := math.Sqrt(zeta*zeta - 1)
	r1, r2 := -w*(zeta-s), -w*(zeta+s)
	return 1 - (r2*math.Exp(r1*t)-r1*math.Exp(r2*t))/(r2-r1)
}

// TrackTiming returns the duration, delay and easing track t moves with
// in a.
func (a Animation) TrackTiming(t Track) (duration, delay float64, easing Easing) {
	duration, delay, easing = a.Duration, a.Delay, a.Easing
	if t.Timing != nil {
		if t.Timing.Duration.Set {
			duration = t.Timing.Duration.V
		}
		if t.Timing.Delay.Set {
			delay += t.Timing.Delay.V
		}
		if t.Timing.Easing != nil {
			easing = *t.Timing.Easing
		}
	}
	return duration, delay, easing
}

// Length is how long a takes, in milliseconds at scale 1, before any
// stagger: its longest track, or its glide.
func (a Animation) Length() float64 {
	if a.Duration <= 0 {
		return 0
	}
	length := a.Delay + a.Duration
	for _, t := range a.Tracks {
		duration, delay, _ := a.TrackTiming(t)
		length = max(length, delay+duration)
	}
	return length
}

// EndsAtRest reports whether each of a's tracks ends where it rests once
// it has come in, as all but a from>to track ending elsewhere do.
func (a Animation) EndsAtRest() bool {
	for _, t := range a.Tracks {
		if t.Pair && t.To != t.Rest() {
			return false
		}
	}
	return true
}

// Moves reports whether a moves anything.
func (a Animation) Moves() bool { return a.Duration > 0 && (a.Glide || len(a.Tracks) > 0) }

// Track returns a's track of prop, if it has one.
func (a Animation) Track(prop string) (Track, bool) {
	for _, t := range a.Tracks {
		if t.Prop == prop {
			return t, true
		}
	}
	return Track{}, false
}

// AnimationPatch is an animation as written: the parts given, to set over
// another. A timing alone changes only the timing.
type AnimationPatch struct {
	None                              bool // no motion: it clears what it is set over
	Duration, Delay, Stagger, Stretch Opt[float64]
	Easing                            *Easing
	Glide                             Opt[bool]
	Tracks                            []Track
	SetTracks                         bool
}

// Apply sets p over a.
func (p *AnimationPatch) Apply(a Animation) Animation {
	if p == nil {
		return a
	}
	if p.None {
		a = Animation{Easing: a.Easing}
	}
	if p.Duration.Set {
		a.Duration = p.Duration.V
	}
	if p.Delay.Set {
		a.Delay = p.Delay.V
	}
	if p.Stagger.Set {
		a.Stagger = p.Stagger.V
	}
	if p.Stretch.Set {
		a.Stretch = p.Stretch.V
	}
	if p.Easing != nil {
		a.Easing = *p.Easing
	}
	if p.Glide.Set {
		a.Glide = p.Glide.V
	}
	if p.SetTracks {
		a.Tracks = p.Tracks // tracks are never changed in place, so they are shared
	}
	return a
}

// Then is p with q set over it.
func (p *AnimationPatch) Then(q *AnimationPatch) *AnimationPatch {
	if p == nil {
		return q
	}
	if q.None {
		return q
	}
	r := *p
	if q.Duration.Set {
		r.Duration = q.Duration
	}
	if q.Delay.Set {
		r.Delay = q.Delay
	}
	if q.Stagger.Set {
		r.Stagger = q.Stagger
	}
	if q.Stretch.Set {
		r.Stretch = q.Stretch
	}
	if q.Easing != nil {
		r.Easing = q.Easing
	}
	if q.Glide.Set {
		r.Glide = q.Glide
	}
	if q.SetTracks {
		r.Tracks, r.SetTracks = q.Tracks, true
	}
	return &r
}

// wholePatch is the patch that sets all of a.
func wholePatch(a Animation) *AnimationPatch {
	if a.Duration <= 0 {
		return &AnimationPatch{None: true}
	}
	e := a.Easing
	p := &AnimationPatch{Duration: some(a.Duration), Easing: &e, Glide: some(a.Glide), Tracks: a.Tracks, SetTracks: true}
	if a.Delay > 0 {
		p.Delay = some(a.Delay)
	}
	if a.Stagger > 0 {
		p.Stagger = some(a.Stagger)
	}
	if a.Stretch > 0 {
		p.Stretch = some(a.Stretch)
	}
	return p
}

// presets are the effects named in a motion: each the tracks it adds, or
// glide.
var presets = map[string]Track{
	"fade":  {Prop: "opacity", From: 0},
	"rise":  {Prop: "y", From: 12, Unit: "px"},
	"drop":  {Prop: "y", From: -12, Unit: "px"},
	"zoom":  {Prop: "scale", From: 0.95},
	"pop":   {Prop: "scale", From: 0.8},
	"slide": {Prop: "slide", Auto: true},
	"wipe":  {Prop: "clip", From: 1},
}

// presetAliases are other names for presets.
var presetAliases = map[string]string{"crossfade": "fade"}

// PresetNames lists the effects a motion can name.
func PresetNames() []string {
	names := []string{"glide"}
	for name := range presets {
		names = append(names, name)
	}
	for name := range presetAliases {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func motionTransition(ms float64, easing string, effects ...string) Animation {
	p, err := parseAnimationText(strings.Join(append(effects, formatNumber(ms)+"ms", easing), " "))
	if err != nil {
		panic(err)
	}
	return p.Apply(Animation{})
}

func both(a Animation) [MomentCount]Animation {
	var m [MomentCount]Animation
	m[MomentEnter], m[MomentExit] = a, a
	return m
}

func changing(a Animation) [MomentCount]Animation {
	var m [MomentCount]Animation
	m[MomentChange] = a
	return m
}

// defaultMotion is quick and quiet: the selection, the cursor and the
// status bar's sides glide to where they go, menus and messages fade
// briefly in and out, and the bars slide along the edges.
var defaultMotion = Motion{
	Scale: 1,
	Moves: Moves{
		MotionPanel:             both(motionTransition(120, "ease-out", "fade")),
		MotionRows:              both(Animation{Easing: namedEasing("ease-out")}),
		MotionSelection:         changing(motionTransition(80, "ease-out", "glide")),
		MotionOverviewSelection: changing(motionTransition(80, "ease-out", "glide")),
		MotionPrompt:            changing(motionTransition(100, "ease-out", "glide")),
		MotionMessage:           both(motionTransition(150, "ease-out", "fade")),
		MotionCursor:            changing(motionTransition(50, "ease-out", "glide")),
		MotionEdges:             both(motionTransition(160, "ease-out", "slide")),
		MotionTitleBar:          both(motionTransition(160, "linear", "fade")),
		MotionTabSelection:      changing(motionTransition(120, "ease-out", "glide")),
		MotionHover:             both(motionTransition(100, "ease-out", "fade")),
		MotionDocument:          changing(motionTransition(120, "ease-out", "fade")),
	},
	Reduced: ReducedMotion{Base: "fade"},
}

// stillMotion is the default motion turned off, for the classic theme.
var stillMotion = func() Motion {
	m := defaultMotion
	m.Scale = 0
	return m
}()

// Animation is target's animation at moment, with motion reduced or not.
func (m *Motion) Animation(target MotionTarget, moment Moment, reduced bool) Animation {
	from := moment
	if moment == MomentKeys && !m.KeysSet[target] {
		from = MomentChange
	}
	a := m.Moves[target][from]
	if !reduced && !m.ForceReduced {
		return a
	}
	switch m.Reduced.Base {
	case "none":
		a = Animation{Easing: a.Easing}
	case "full":
	default:
		a = fadeOnly(target, a)
	}
	a = m.Reduced.Patches[target][from].Apply(a)
	if from != moment {
		a = m.Reduced.Patches[target][moment].Apply(a)
	}
	return a
}

// Active is the motion to use, and its scale, with motion reduced or not.
func (m *Motion) Active(reduced bool) (Moves, float64) {
	if !reduced && !m.ForceReduced {
		moves := m.Moves
		for t := range moves {
			if !m.KeysSet[t] {
				moves[t][MomentKeys] = moves[t][MomentChange]
			}
		}
		return moves, m.Scale
	}
	var moves Moves
	for t := range moves {
		for moment := range moves[t] {
			moves[t][moment] = m.Animation(MotionTarget(t), Moment(moment), true)
		}
	}
	scale := m.Scale
	if m.Reduced.Scale.Set {
		scale = m.Reduced.Scale.V
	}
	return moves, scale
}

// fadeOnly is a with only its fades: nothing glides, slides or grows.
// The edges move the page, so they are still.
func fadeOnly(t MotionTarget, a Animation) Animation {
	if t == MotionEdges {
		return Animation{Easing: a.Easing}
	}
	a.Glide = false
	if slices.ContainsFunc(a.Tracks, func(t Track) bool { return t.Prop != "opacity" }) {
		a.Tracks = slices.DeleteFunc(slices.Clone(a.Tracks), func(t Track) bool { return t.Prop != "opacity" })
	}
	return a
}

// --- Reading motion as written ----------------------------------------

// motionWords splits text into words, keeping what is in brackets, such
// as cubic-bezier(0.2, 0, 0, 1), as one.
func motionWords(text string) []string {
	var words []string
	var word strings.Builder
	depth := 0
	for _, r := range text {
		switch {
		case r == '(':
			depth++
		case r == ')':
			depth = max(0, depth-1)
		case depth == 0 && (r == ' ' || r == '\t' || r == '\n'):
			if word.Len() > 0 {
				words = append(words, word.String())
				word.Reset()
			}
			continue
		case depth > 0 && r == ' ':
			continue
		}
		word.WriteRune(r)
	}
	if word.Len() > 0 {
		words = append(words, word.String())
	}
	return words
}

const motionUsage = `expected effects and a timing such as "fade rise 140ms ease-out", or "none"`

// parseAnimationText reads an animation written as words: effects (fade,
// slide, glide, ...) and tracks (opacity:0, y:8px, scale:0.9>1), a
// duration and then a delay, an easing, and delay:, stagger: and from:
// (the side a slide or wipe goes to), in any order; or "none". Without
// effects or tracks it sets only the timing; a duration without an easing
// eases as CSS does by default.
func parseAnimationText(raw string) (*AnimationPatch, error) {
	text := strings.ToLower(strings.TrimSpace(raw))
	if text == "none" || text == "" {
		return &AnimationPatch{None: true}, nil
	}
	p := &AnimationPatch{}
	side := ""
	durations := 0
	glide := false
	for _, word := range motionWords(text) {
		if d, ok := parseDuration(word); ok {
			switch durations {
			case 0:
				p.Duration = some(d)
			case 1:
				p.Delay = some(d)
			default:
				return nil, fmt.Errorf("%q: more than a duration and a delay", raw)
			}
			durations++
			continue
		}
		if e, err := parseEasingName(word); err == nil {
			if p.Easing != nil {
				return nil, fmt.Errorf("%q: more than one easing", raw)
			}
			p.Easing = &e
			continue
		} else if strings.Contains(word, "(") {
			return nil, err
		}
		if word == "glide" {
			glide, p.SetTracks = true, true
			continue
		}
		name := word
		if alias, ok := presetAliases[name]; ok {
			name = alias
		}
		if track, ok := presets[name]; ok {
			p.Tracks, p.SetTracks = setTrack(p.Tracks, track), true
			continue
		}
		key, value, ok := strings.Cut(word, ":")
		if !ok {
			return nil, fmt.Errorf("%q: unknown word %q; %s, with effects %s", raw, word, motionUsage, strings.Join(PresetNames(), ", "))
		}
		switch key {
		case "delay", "stagger", "stretch":
			d, ok := parseDuration(value)
			if !ok {
				return nil, fmt.Errorf("%q: %s: expected a duration such as 40ms", raw, key)
			}
			switch key {
			case "delay":
				p.Delay = some(d)
			case "stagger":
				p.Stagger = some(d)
			default:
				p.Stretch = some(d)
			}
		case "from":
			if !slices.Contains(motionSides, value) {
				return nil, fmt.Errorf("%q: from: expected one of %s", raw, strings.Join(motionSides, ", "))
			}
			side = value
		default:
			if _, ok := trackProps[key]; !ok {
				return nil, fmt.Errorf("%q: unknown word %q; tracks are %s", raw, word, strings.Join(trackOrder, ", "))
			}
			track, err := parseTrackText(key, value)
			if err != nil {
				return nil, fmt.Errorf("%q: %w", raw, err)
			}
			p.Tracks, p.SetTracks = setTrack(p.Tracks, track), true
		}
	}
	if side != "" {
		for i := range p.Tracks {
			if p.Tracks[i].Prop == "slide" || p.Tracks[i].Prop == "clip" {
				p.Tracks[i].Side = side
			}
		}
	}
	if p.SetTracks {
		p.Glide = some(glide)
	}
	if p.Duration.Set && p.Easing == nil {
		e := namedEasing("ease")
		p.Easing = &e
	}
	if !p.Duration.Set && !p.SetTracks && p.Easing == nil && !p.Delay.Set && !p.Stagger.Set && !p.Stretch.Set {
		return nil, fmt.Errorf("%q: %s", raw, motionUsage)
	}
	if !p.Duration.Set && !p.SetTracks && p.Easing != nil {
		return nil, fmt.Errorf("%q: expected a duration, such as 140ms", raw)
	}
	return p, nil
}

// motionSides are the sides a slide or wipe can go to.
var motionSides = []string{"top", "bottom", "left", "right"}

// setTrack adds t to tracks, in place of one of the same property.
func setTrack(tracks []Track, t Track) []Track {
	tracks = slices.DeleteFunc(slices.Clone(tracks), func(o Track) bool { return o.Prop == t.Prop })
	tracks = append(tracks, t)
	slices.SortStableFunc(tracks, func(a, b Track) int {
		return slices.Index(trackOrder, a.Prop) - slices.Index(trackOrder, b.Prop)
	})
	return tracks
}

// parseTrackText reads a track's value as written after its name: one
// value, the hidden one, or from>to; a slide may be auto or a side.
func parseTrackText(prop, value string) (Track, error) {
	t := Track{Prop: prop}
	if prop == "slide" || prop == "clip" {
		if slices.Contains(motionSides, value) {
			t.Side = value
			if prop == "slide" {
				t.Auto = true
			} else {
				t.From = 1
			}
			return t, nil
		}
		if prop == "slide" && value == "auto" {
			t.Auto = true
			return t, nil
		}
	}
	from, to, pair := strings.Cut(value, ">")
	v, unit, err := parseTrackValue(prop, from)
	if err != nil {
		return t, err
	}
	t.From, t.Unit = v, unit
	if pair {
		w, unit2, err := parseTrackValue(prop, to)
		if err != nil {
			return t, err
		}
		if unit2 != "" && unit != "" && unit2 != unit {
			return t, fmt.Errorf("%s: from and to need the same unit", prop)
		}
		t.To, t.Pair = w, true
		if t.Unit == "" {
			t.Unit = unit2
		}
	}
	if (prop == "x" || prop == "y" || prop == "slide") && t.Unit == "" {
		t.Unit = "px"
	}
	return t, nil
}

// parseTrackValue reads one value of a track: a number, with px or % for
// the properties that take a length.
func parseTrackValue(prop, s string) (float64, string, error) {
	unit := ""
	length := prop == "x" || prop == "y" || prop == "slide"
	switch {
	case length && strings.HasSuffix(s, "px"):
		s, unit = strings.TrimSuffix(s, "px"), "px"
	case length && strings.HasSuffix(s, "%"):
		s, unit = strings.TrimSuffix(s, "%"), "%"
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		if length {
			return 0, "", fmt.Errorf("%s: expected a length such as 8px or 50%%", prop)
		}
		return 0, "", fmt.Errorf("%s: expected a number", prop)
	}
	return v, unit, nil
}

// parseDuration reads "140ms", "0.14s" or a bare number of milliseconds.
func parseDuration(s string) (float64, bool) {
	scale := 1.0
	switch {
	case strings.HasSuffix(s, "ms"):
		s = strings.TrimSuffix(s, "ms")
	case strings.HasSuffix(s, "s"):
		s, scale = strings.TrimSuffix(s, "s"), 1000
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 || math.IsInf(v, 0) || math.IsNaN(v) {
		return 0, false
	}
	return v * scale, true
}

const easingUsage = "expected linear, ease, ease-in, ease-out, ease-in-out, cubic-bezier(x1, y1, x2, y2) with x1 and x2 from 0 to 1, steps(n), spring(stiffness, damping) or a function"

// parseEasingName reads a named easing, cubic-bezier(x1, y1, x2, y2),
// steps(n) or spring(stiffness, damping).
func parseEasingName(easing string) (Easing, error) {
	easing = strings.ToLower(strings.TrimSpace(easing))
	if curve, ok := easings[easing]; ok {
		return Easing{Name: easing, Curve: curve}, nil
	}
	fn, args, ok := strings.Cut(easing, "(")
	args, ok2 := strings.CutSuffix(args, ")")
	if !ok || !ok2 {
		return Easing{}, fmt.Errorf("unknown easing %q; %s", easing, easingUsage)
	}
	var v []float64
	for part := range strings.SplitSeq(args, ",") {
		f, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil {
			return Easing{}, fmt.Errorf("%s: %q is not a number", easing, part)
		}
		v = append(v, f)
	}
	switch {
	case fn == "cubic-bezier" && len(v) == 4 && v[0] >= 0 && v[0] <= 1 && v[2] >= 0 && v[2] <= 1:
		return Easing{Name: fmt.Sprintf("cubic-bezier(%s, %s, %s, %s)", formatNumber(v[0]), formatNumber(v[1]), formatNumber(v[2]), formatNumber(v[3])), Curve: [4]float64(v)}, nil
	case fn == "steps" && len(v) == 1 && v[0] >= 1 && v[0] == math.Trunc(v[0]):
		return Easing{Name: fmt.Sprintf("steps(%d)", int(v[0])), Steps: int(v[0])}, nil
	case fn == "spring" && len(v) == 2 && v[0] > 0 && v[1] > 0:
		return Easing{Name: fmt.Sprintf("spring(%s, %s)", formatNumber(v[0]), formatNumber(v[1])), Spring: [2]float64(v)}, nil
	}
	return Easing{}, fmt.Errorf("unknown easing %q; %s", easing, easingUsage)
}

// easingFromLua reads an easing written as text or a Lua function of the
// time through the change, from 0 to 1, returning how far through it is.
func easingFromLua(value lua.LValue) (Easing, error) {
	switch value := value.(type) {
	case lua.LString:
		return parseEasingName(string(value))
	case *lua.LFunction:
		return Easing{Name: "function", Func: value}, nil
	}
	return Easing{}, fmt.Errorf("easing: %s", easingUsage)
}

// durationFromLua reads a duration: a number of milliseconds, or text
// such as "140ms".
func durationFromLua(name string, value lua.LValue) (float64, error) {
	switch value := value.(type) {
	case lua.LNumber:
		return max(0, float64(value)), nil
	case lua.LString:
		if d, ok := parseDuration(strings.TrimSpace(string(value))); ok {
			return d, nil
		}
	}
	return 0, fmt.Errorf("%s: expected a duration such as 140 or \"140ms\"", name)
}

// animationTableKeys are the keys an animation's table may have besides
// its tracks.
var animationTableKeys = []string{"duration", "delay", "stagger", "stretch", "easing", "glide", "effect", "from"}

// AnimationFromLua reads an animation: text, a number of milliseconds, or
// a table of duration, delay, stagger, easing, glide, effect (text, as an
// animation is written) and tracks, each a value, {from, to}, or a table
// with its own duration, delay and easing.
func AnimationFromLua(value lua.LValue) (*AnimationPatch, error) {
	switch value := value.(type) {
	case lua.LString:
		return parseAnimationText(string(value))
	case lua.LNumber:
		return &AnimationPatch{Duration: some(max(0, float64(value)))}, nil
	case lua.LBool:
		if !value {
			return &AnimationPatch{None: true}, nil
		}
	case *lua.LTable:
		return animationFromTable(value)
	}
	return nil, fmt.Errorf(`%s, or a table such as { duration = 140, easing = "ease-out", opacity = 0 }`, motionUsage)
}

func animationFromTable(tbl *lua.LTable) (*AnimationPatch, error) {
	p := &AnimationPatch{}
	if effect, ok := tbl.RawGetString("effect").(lua.LString); ok {
		ep, err := parseAnimationText(string(effect))
		if err != nil {
			return nil, fmt.Errorf("effect: %w", err)
		}
		p = ep
	} else if v := tbl.RawGetString("effect"); v != lua.LNil {
		return nil, fmt.Errorf("effect: expected text such as \"fade rise\"")
	}
	for _, name := range []string{"duration", "delay", "stagger", "stretch"} {
		v := tbl.RawGetString(name)
		if v == lua.LNil {
			continue
		}
		d, err := durationFromLua(name, v)
		if err != nil {
			return nil, err
		}
		switch name {
		case "duration":
			p.Duration, p.None = some(d), false
		case "delay":
			p.Delay = some(d)
		case "stagger":
			p.Stagger = some(d)
		case "stretch":
			p.Stretch = some(d)
		}
	}
	if v := tbl.RawGetString("easing"); v != lua.LNil {
		e, err := easingFromLua(v)
		if err != nil {
			return nil, err
		}
		p.Easing = &e
	}
	if v := tbl.RawGetString("glide"); v != lua.LNil {
		b, ok := v.(lua.LBool)
		if !ok {
			return nil, fmt.Errorf("glide: expected true or false")
		}
		p.Glide = some(bool(b))
		if !p.SetTracks {
			p.SetTracks = true
		}
	}
	for _, prop := range trackOrder {
		v := tbl.RawGetString(prop)
		if v == lua.LNil {
			continue
		}
		t, err := trackFromLua(prop, v)
		if err != nil {
			return nil, err
		}
		p.Tracks, p.SetTracks = setTrack(p.Tracks, t), true
	}
	if p.SetTracks && !p.Glide.Set {
		p.Glide = some(false)
	}
	if side, ok := tbl.RawGetString("from").(lua.LString); ok {
		if !slices.Contains(motionSides, string(side)) {
			return nil, fmt.Errorf("from: expected one of %s", strings.Join(motionSides, ", "))
		}
		for i := range p.Tracks {
			if p.Tracks[i].Prop == "slide" || p.Tracks[i].Prop == "clip" {
				p.Tracks[i].Side = string(side)
			}
		}
	}
	var err error
	tbl.ForEach(func(key, _ lua.LValue) {
		name, ok := key.(lua.LString)
		_, isTrack := trackProps[string(name)]
		if err == nil && (!ok || !slices.Contains(animationTableKeys, string(name)) && !isTrack) {
			err = fmt.Errorf("unknown key %v; an animation has %s and the tracks %s", key, strings.Join(animationTableKeys, ", "), strings.Join(trackOrder, ", "))
		}
	})
	return p, err
}

// trackFromLua reads a track: a value, text as written after its name in
// text ("8px", "0>1", "left"), {from, to}, or a table with from, to,
// duration, delay, easing and, for slide and clip, side.
func trackFromLua(prop string, value lua.LValue) (Track, error) {
	text := func(v lua.LValue) (string, error) {
		switch v := v.(type) {
		case lua.LNumber:
			return formatNumber(float64(v)), nil
		case lua.LString:
			return strings.ToLower(strings.TrimSpace(string(v))), nil
		}
		return "", fmt.Errorf("%s: expected a number or text such as \"8px\"", prop)
	}
	switch value := value.(type) {
	case lua.LNumber, lua.LString:
		s, err := text(value)
		if err != nil {
			return Track{}, err
		}
		return parseTrackText(prop, s)
	case *lua.LTable:
		from := value.RawGetString("from")
		if from == lua.LNil {
			from = value.RawGetInt(1)
		}
		to := value.RawGetString("to")
		if to == lua.LNil {
			to = value.RawGetInt(2)
		}
		var t Track
		if from != lua.LNil {
			s, err := text(from)
			if err != nil {
				return Track{}, err
			}
			if to != lua.LNil {
				s2, err := text(to)
				if err != nil {
					return Track{}, err
				}
				s += ">" + s2
			}
			if t, err = parseTrackText(prop, s); err != nil {
				return Track{}, err
			}
		} else if def, ok := presets[prop]; ok {
			t = def
		} else {
			t = Track{Prop: prop, From: trackProps[prop]}
			if prop == "slide" {
				t.Auto = true
			} else if prop == "clip" {
				t.From = 1
			}
		}
		if side, ok := value.RawGetString("side").(lua.LString); ok {
			if !slices.Contains(motionSides, string(side)) {
				return Track{}, fmt.Errorf("%s: side: expected one of %s", prop, strings.Join(motionSides, ", "))
			}
			t.Side = string(side)
		}
		timing := &TrackTiming{}
		given := false
		for _, name := range []string{"duration", "delay"} {
			if v := value.RawGetString(name); v != lua.LNil {
				d, err := durationFromLua(prop+"."+name, v)
				if err != nil {
					return Track{}, err
				}
				if name == "duration" {
					timing.Duration = some(d)
				} else {
					timing.Delay = some(d)
				}
				given = true
			}
		}
		if v := value.RawGetString("easing"); v != lua.LNil {
			e, err := easingFromLua(v)
			if err != nil {
				return Track{}, fmt.Errorf("%s.%w", prop, err)
			}
			timing.Easing, given = &e, true
		}
		if given {
			t.Timing = timing
		}
		return t, nil
	}
	return Track{}, fmt.Errorf("%s: expected a value, {from, to}, or a table", prop)
}

// TargetPatch is what a target's motion sets, for each moment.
type TargetPatch [MomentCount]*AnimationPatch

// targetPatchFromLua reads a target's motion: an animation for all its
// moments, or a table of them by moment, such as { enter = "fade",
// exit = "none" }.
func targetPatchFromLua(t MotionTarget, value lua.LValue) (TargetPatch, error) {
	var tp TargetPatch
	if tbl, ok := value.(*lua.LTable); ok {
		byMoment := false
		for _, name := range momentNames {
			if tbl.RawGetString(name) != lua.LNil {
				byMoment = true
			}
		}
		if byMoment {
			var err error
			tbl.ForEach(func(key, v lua.LValue) {
				if err != nil {
					return
				}
				name := strings.ToLower(lua.LVAsString(key))
				moment := Moment(slices.Index(momentNames[:], name))
				if moment < 0 {
					err = fmt.Errorf("unknown moment %v; %s moves at %s", key, t, momentList(t))
					return
				}
				if !slices.Contains(t.AllMoments(), moment) {
					err = fmt.Errorf("%s moves at %s, not %s", t, momentList(t), name)
					return
				}
				p, e := AnimationFromLua(v)
				if e != nil {
					err = fmt.Errorf("%s: %w", name, e)
					return
				}
				tp[moment] = p
			})
			return tp, err
		}
	}
	p, err := AnimationFromLua(value)
	if err != nil {
		return tp, err
	}
	for _, moment := range t.Moments() {
		tp[moment] = p
	}
	return tp, nil
}

func momentList(t MotionTarget) string {
	var names []string
	for _, m := range t.AllMoments() {
		names = append(names, m.String())
	}
	return strings.Join(names, " and ")
}

// --- Writing motion ----------------------------------------------------

// animationText writes p as text, or reports that text cannot say it: a
// track with its own timing, a Lua easing, or a timing without its
// duration.
func animationText(p *AnimationPatch) (string, bool) {
	if p == nil {
		return "", false
	}
	if p.None {
		if p.Duration.Set && p.Duration.V > 0 {
			return "", false
		}
		return "none", true
	}
	if p.Duration.Set && p.Duration.V == 0 && !p.SetTracks {
		return "none", true
	}
	var words []string
	if p.SetTracks {
		if p.Glide.V {
			words = append(words, "glide")
		}
		side := ""
		for _, t := range p.Tracks {
			if t.Timing != nil {
				return "", false
			}
			word := trackText(t)
			if t.Side != "" && (t.Prop == "slide" || t.Prop == "clip") {
				if side != "" && side != t.Side {
					return "", false
				}
				side = t.Side
			}
			words = append(words, word)
		}
		if side != "" {
			words = append(words, "from:"+side)
		}
	}
	if p.Easing != nil && p.Easing.Func != nil {
		return "", false
	}
	if p.Duration.Set {
		words = append(words, formatNumber(p.Duration.V)+"ms")
	} else if p.Easing != nil {
		return "", false
	}
	if p.Easing != nil {
		words = append(words, p.Easing.Name)
	}
	if p.Delay.Set && p.Delay.V > 0 {
		words = append(words, "delay:"+formatNumber(p.Delay.V)+"ms")
	}
	if p.Stagger.Set && p.Stagger.V > 0 {
		words = append(words, "stagger:"+formatNumber(p.Stagger.V)+"ms")
	}
	if p.Stretch.Set && p.Stretch.V > 0 {
		words = append(words, "stretch:"+formatNumber(p.Stretch.V)+"ms")
	}
	if len(words) == 0 {
		return "", false
	}
	return strings.Join(words, " "), true
}

// trackText writes t as a word: a preset's name where it is one.
func trackText(t Track) string {
	plain := t
	plain.Side = ""
	for _, name := range []string{"fade", "rise", "drop", "zoom", "pop", "slide", "wipe"} {
		if presets[name] == plain {
			return name
		}
	}
	value := func(v float64) string {
		if t.Unit == "%" {
			return formatNumber(v) + "%"
		}
		if t.Unit == "px" {
			return formatNumber(v) + "px"
		}
		return formatNumber(v)
	}
	if t.Auto {
		return t.Prop + ":auto"
	}
	if t.Pair {
		return t.Prop + ":" + value(t.From) + ">" + value(t.To)
	}
	return t.Prop + ":" + value(t.From)
}

// motionLit is a value as Lua writes it: text, or a table of fields.
type motionLit struct {
	text   string
	isText bool
	fn     *lua.LFunction
	num    *float64
	fields []motionField
}

type motionField struct {
	key   string
	value motionLit
}

func litText(s string) motionLit          { return motionLit{text: s, isText: true} }
func litNum(v float64) motionLit          { return motionLit{num: &v} }
func litTable(f ...motionField) motionLit { return motionLit{fields: f} }

// animationLit is p as Lua writes it: text where text can say it.
func animationLit(p *AnimationPatch) motionLit {
	if s, ok := animationText(p); ok {
		return litText(s)
	}
	var f []motionField
	if p.SetTracks {
		if p.Glide.V {
			f = append(f, motionField{"glide", litText("true")})
		}
		for _, t := range p.Tracks {
			f = append(f, motionField{t.Prop, trackLit(t)})
		}
	}
	if p.Duration.Set {
		f = append(f, motionField{"duration", litNum(p.Duration.V)})
	}
	if p.Delay.Set && p.Delay.V > 0 {
		f = append(f, motionField{"delay", litNum(p.Delay.V)})
	}
	if p.Stagger.Set && p.Stagger.V > 0 {
		f = append(f, motionField{"stagger", litNum(p.Stagger.V)})
	}
	if p.Stretch.Set && p.Stretch.V > 0 {
		f = append(f, motionField{"stretch", litNum(p.Stretch.V)})
	}
	if p.Easing != nil {
		f = append(f, motionField{"easing", easingLit(*p.Easing)})
	}
	return litTable(f...)
}

func easingLit(e Easing) motionLit {
	if e.Func != nil {
		return motionLit{fn: e.Func}
	}
	return litText(e.Name)
}

func trackLit(t Track) motionLit {
	value := func(v float64) motionLit {
		if t.Unit == "%" {
			return litText(formatNumber(v) + "%")
		}
		return litNum(v)
	}
	var f []motionField
	switch {
	case t.Auto:
		f = append(f, motionField{"from", litText("auto")})
	case t.Pair:
		f = append(f, motionField{"from", value(t.From)}, motionField{"to", value(t.To)})
	default:
		f = append(f, motionField{"from", value(t.From)})
	}
	if t.Side != "" {
		f = append(f, motionField{"side", litText(t.Side)})
	}
	if t.Timing != nil {
		if t.Timing.Duration.Set {
			f = append(f, motionField{"duration", litNum(t.Timing.Duration.V)})
		}
		if t.Timing.Delay.Set {
			f = append(f, motionField{"delay", litNum(t.Timing.Delay.V)})
		}
		if t.Timing.Easing != nil {
			f = append(f, motionField{"easing", easingLit(*t.Timing.Easing)})
		}
	}
	return litTable(f...)
}

// targetLit is a target's motion: one animation where its moments share
// it, or a table of them by moment. Moments not set are left out.
func targetLit(t MotionTarget, tp TargetPatch) (motionLit, bool) {
	var lits []motionField
	for _, m := range t.AllMoments() {
		if tp[m] != nil {
			lits = append(lits, motionField{m.String(), animationLit(tp[m])})
		}
	}
	if len(lits) == 0 {
		return motionLit{}, false
	}
	if len(lits) == len(t.Moments()) {
		first := lits[0].value.write()
		same := true
		for _, l := range lits[1:] {
			same = same && l.value.write() == first
		}
		if same {
			return lits[0].value, true
		}
	}
	return litTable(lits...), true
}

func (l motionLit) write() string {
	switch {
	case l.isText && (l.text == "true" || l.text == "false"):
		return l.text
	case l.isText:
		return strconv.Quote(l.text)
	case l.num != nil:
		return formatNumber(*l.num)
	case l.fn != nil:
		return "function"
	}
	parts := make([]string, len(l.fields))
	for i, f := range l.fields {
		parts[i] = f.key + " = " + f.value.write()
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

func (l motionLit) lua(L *lua.LState) lua.LValue {
	switch {
	case l.isText && (l.text == "true" || l.text == "false"):
		return lua.LBool(l.text == "true")
	case l.isText:
		return lua.LString(l.text)
	case l.num != nil:
		return lua.LNumber(*l.num)
	case l.fn != nil:
		return l.fn
	}
	tbl := L.NewTable()
	for _, f := range l.fields {
		tbl.RawSetString(f.key, f.value.lua(L))
	}
	return tbl
}

// wholeTarget is all of t's motion in m: the moments set only by name
// where they are, keys once given and press once it moves.
func wholeTarget(m *Motion, t MotionTarget) TargetPatch {
	var tp TargetPatch
	for _, moment := range t.Moments() {
		tp[moment] = wholePatch(m.Moves[t][moment])
	}
	for _, moment := range motionTargets[t].named {
		if moment == MomentKeys && m.KeysSet[t] || moment == MomentPress && m.Moves[t][moment].Moves() {
			tp[moment] = wholePatch(m.Moves[t][moment])
		}
	}
	return tp
}

// --- Options -----------------------------------------------------------

// motionTargetOption holds a target's motion in the motion table.
func motionTargetOption(t MotionTarget) optionDesc {
	return optionDesc{
		kind:        "motion",
		description: motionTargets[t].description,
		get: func(L *lua.LState, cfg *Config) lua.LValue {
			lit, _ := targetLit(t, wholeTarget(&cfg.Theme.Motion, t))
			return lit.lua(L)
		},
		format: func(cfg *Config) string {
			lit, _ := targetLit(t, wholeTarget(&cfg.Theme.Motion, t))
			return lit.write()
		},
		applyText: func(cfg *Config, raw string) error {
			raw, err := parseStringOption(raw)
			if err != nil {
				return err
			}
			p, err := parseAnimationText(raw)
			if err != nil {
				return err
			}
			for _, moment := range t.Moments() {
				cfg.Theme.Motion.Moves[t][moment] = p.Apply(cfg.Theme.Motion.Moves[t][moment])
			}
			return nil
		},
		apply: func(cfg *Config, value lua.LValue) error {
			tp, err := targetPatchFromLua(t, value)
			if err != nil {
				return err
			}
			if tp[MomentKeys] != nil && !cfg.Theme.Motion.KeysSet[t] {
				// keys starts from change, which it followed until now.
				cfg.Theme.Motion.KeysSet[t] = true
				cfg.Theme.Motion.Moves[t][MomentKeys] = cfg.Theme.Motion.Moves[t][MomentChange]
				if tp[MomentChange] != nil {
					cfg.Theme.Motion.Moves[t][MomentKeys] = tp[MomentChange].Apply(cfg.Theme.Motion.Moves[t][MomentKeys])
				}
			}
			for moment, p := range tp {
				cfg.Theme.Motion.Moves[t][moment] = p.Apply(cfg.Theme.Motion.Moves[t][moment])
			}
			return nil
		},
	}
}

// reducedTargetOption holds a target's motion while motion is reduced,
// over what the reduced base gives it.
func reducedTargetOption(t MotionTarget) optionDesc {
	patches := func(cfg *Config) *[MomentCount]*AnimationPatch { return &cfg.Theme.Motion.Reduced.Patches[t] }
	set := func(cfg *Config, tp TargetPatch) {
		for moment, p := range tp {
			if p != nil {
				patches(cfg)[moment] = patches(cfg)[moment].Then(p)
			}
		}
	}
	return optionDesc{
		kind:        "motion",
		description: "As " + t.String() + ", while motion is reduced.",
		get: func(L *lua.LState, cfg *Config) lua.LValue {
			lit, ok := targetLit(t, TargetPatch(*patches(cfg)))
			if !ok {
				return lua.LNil
			}
			return lit.lua(L)
		},
		format: func(cfg *Config) string {
			lit, ok := targetLit(t, TargetPatch(*patches(cfg)))
			if !ok {
				return "nil"
			}
			return lit.write()
		},
		applyText: func(cfg *Config, raw string) error {
			raw, err := parseStringOption(raw)
			if err != nil {
				return err
			}
			p, err := parseAnimationText(raw)
			if err != nil {
				return err
			}
			var tp TargetPatch
			for _, moment := range t.Moments() {
				tp[moment] = p
			}
			set(cfg, tp)
			return nil
		},
		apply: func(cfg *Config, value lua.LValue) error {
			if value == lua.LNil {
				*patches(cfg) = [MomentCount]*AnimationPatch{}
				return nil
			}
			tp, err := targetPatchFromLua(t, value)
			if err != nil {
				return err
			}
			set(cfg, tp)
			return nil
		},
	}
}

var reducedBases = []string{"fade", "none", "full"}

func parseReducedBase(raw string) (string, error) {
	base := strings.ToLower(strings.TrimSpace(raw))
	if !slices.Contains(reducedBases, base) {
		return "", fmt.Errorf("%q: expected fade, none, full or a motion table", raw)
	}
	return base, nil
}

// reducedOption holds motion.reduced written as text: the base alone,
// which drops what was set over it.
func reducedOption(clear bool) optionDesc {
	set := func(cfg *Config, raw string) error {
		base, err := parseReducedBase(raw)
		if err != nil {
			return err
		}
		if clear {
			cfg.Theme.Motion.Reduced = ReducedMotion{Base: base}
		} else {
			cfg.Theme.Motion.Reduced.Base = base
		}
		return nil
	}
	description := "The motion while motion is reduced: fade, the motion with only its fades; none; full, the motion as it is; or a motion table, over base."
	if !clear {
		description = "What reduced motion starts from: fade, the motion with only its fades; none; or full, the motion as it is."
	}
	return optionDesc{
		kind:        "string",
		description: description,
		get:         func(L *lua.LState, cfg *Config) lua.LValue { return lua.LString(cfg.Theme.Motion.Reduced.Base) },
		format:      func(cfg *Config) string { return strconv.Quote(cfg.Theme.Motion.Reduced.Base) },
		applyText: func(cfg *Config, raw string) error {
			raw, err := parseStringOption(raw)
			if err != nil {
				return err
			}
			return set(cfg, raw)
		},
		apply: func(cfg *Config, value lua.LValue) error {
			s, ok := value.(lua.LString)
			if !ok {
				return fmt.Errorf("expected fade, none, full or a motion table")
			}
			return set(cfg, string(s))
		},
	}
}

// motionThemeFields are the motion table's fields.
func motionThemeFields() []themeField {
	fields := []themeField{
		{"motion.scale", floatOption("Speed of every animation: 1 as given, 2 twice as slow, 0 for no motion at all.", func(c *Config) float64 { return c.Theme.Motion.Scale }, func(c *Config, v float64) { c.Theme.Motion.Scale = max(0, v) })},
		{"motion.force_reduced", boolOption("Use reduced motion whether or not the OS asks for less motion.", func(c *Config) bool { return c.Theme.Motion.ForceReduced }, func(c *Config, v bool) { c.Theme.Motion.ForceReduced = v })},
	}
	for t := range MotionTargetCount {
		fields = append(fields, themeField{"motion." + t.String(), motionTargetOption(t)})
	}
	fields = append(fields,
		themeField{"motion.reduced", reducedOption(true)},
		themeField{"motion.reduced.base", reducedOption(false)},
		themeField{"motion.reduced.scale", optionDesc{
			kind:        "number",
			description: "Speed of every animation while motion is reduced; the motion's scale unless set.",
			get: func(L *lua.LState, cfg *Config) lua.LValue {
				if s := cfg.Theme.Motion.Reduced.Scale; s.Set {
					return lua.LNumber(s.V)
				}
				return lua.LNil
			},
			format: func(cfg *Config) string {
				if s := cfg.Theme.Motion.Reduced.Scale; s.Set {
					return formatNumber(s.V)
				}
				return "nil"
			},
			applyText: func(cfg *Config, raw string) error {
				v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
				if err != nil {
					return fmt.Errorf("expected number")
				}
				cfg.Theme.Motion.Reduced.Scale = some(max(0, v))
				return nil
			},
			apply: func(cfg *Config, value lua.LValue) error {
				switch value := value.(type) {
				case lua.LNumber:
					cfg.Theme.Motion.Reduced.Scale = some(max(0, float64(value)))
				case *lua.LNilType:
					cfg.Theme.Motion.Reduced.Scale = Opt[float64]{}
				default:
					return fmt.Errorf("expected number")
				}
				return nil
			},
		}},
	)
	for t := range MotionTargetCount {
		fields = append(fields, themeField{"motion.reduced." + t.String(), reducedTargetOption(t)})
	}
	return fields
}

// ViewMotion is a view's own motion, over the theme's, for itself and
// what moves in it: its rows, its selection and its cursor.
type ViewMotion [MotionTargetCount]TargetPatch

// viewMotionTargets are the targets a view's motion can set.
var viewMotionTargets = []MotionTarget{MotionPanel, MotionRows, MotionSelection, MotionCursor}

// ViewMotionFromLua reads a view's motion: the panel's, as a target's is
// written, or a table with enter and exit for the view and panel, rows,
// selection and cursor for what moves in it.
func ViewMotionFromLua(L *lua.LState, value lua.LValue) (*ViewMotion, error) {
	vm := &ViewMotion{}
	tbl, ok := value.(*lua.LTable)
	if !ok {
		tp, err := targetPatchFromLua(MotionPanel, value)
		vm[MotionPanel] = tp
		return vm, err
	}
	panel := L.NewTable()
	panelKeys := 0
	var err error
	tbl.ForEach(func(key, v lua.LValue) {
		if err != nil {
			return
		}
		name := strings.ToLower(lua.LVAsString(key))
		if t, ok := MotionTargetNamed(name); ok && slices.Contains(viewMotionTargets, t) {
			tp, e := targetPatchFromLua(t, v)
			if e != nil {
				err = fmt.Errorf("%s: %w", name, e)
				return
			}
			for m, p := range tp {
				if p != nil {
					vm[t][m] = vm[t][m].Then(p)
				}
			}
			return
		}
		panel.RawSetString(name, v)
		panelKeys++
	})
	if err != nil {
		return nil, err
	}
	if panelKeys > 0 {
		tp, err := targetPatchFromLua(MotionPanel, panel)
		if err != nil {
			return nil, err
		}
		for m, p := range tp {
			if p != nil {
				vm[MotionPanel][m] = vm[MotionPanel][m].Then(p)
			}
		}
	}
	return vm, nil
}
