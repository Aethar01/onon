package config

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

// Motion is how the UI moves: a transition for each kind of change, all
// slowed or sped by Scale, and none at all with Scale 0.
type Motion struct {
	Scale     float64
	Panel     Transition // menus opening and closing
	Selection Transition // the selected row's highlight moving to a new row
	Prompt    Transition // the status bar's sides growing and shrinking
	Message   Transition // messages fading in and out
	Cursor    Transition // the prompt's cursor moving
	Edges     Transition // bars and docked views sliding in and out
}

// Transition is how long a change takes and how it eases, as a CSS
// transition, such as "140ms ease-out".
type Transition struct {
	Duration float64    // milliseconds; 0 for none
	Easing   string     // as written
	Curve    [4]float64 // the easing's cubic-bezier control points
}

// easings are the named easings, as cubic-bezier control points.
var easings = map[string][4]float64{
	"linear":      {0, 0, 1, 1},
	"ease":        {0.25, 0.1, 0.25, 1},
	"ease-in":     {0.42, 0, 1, 1},
	"ease-out":    {0, 0, 0.58, 1},
	"ease-in-out": {0.42, 0, 0.58, 1},
}

func transition(ms float64, easing string) Transition {
	t, err := parseEasing(Transition{Duration: ms}, easing)
	if err != nil {
		panic(err)
	}
	return t
}

// defaultMotion is quick and quiet: the selection, the cursor and the
// status bar's sides glide to where they go, menus and messages fade
// briefly in and out, and the bars slide along the edges.
var defaultMotion = Motion{
	Scale:     1,
	Panel:     transition(120, "ease-out"),
	Selection: transition(80, "ease-out"),
	Prompt:    transition(100, "ease-out"),
	Message:   transition(150, "ease-out"),
	Cursor:    transition(50, "ease-out"),
	Edges:     transition(160, "ease-out"),
}

// stillMotion is the default motion turned off, for the classic theme.
var stillMotion = func() Motion {
	m := defaultMotion
	m.Scale = 0
	return m
}()

// parseTransition reads a transition written as CSS writes one: a
// duration in ms or s and an easing, in either order, or "none".
func parseTransition(raw string) (Transition, error) {
	raw, err := parseStringOption(raw)
	if err != nil {
		return Transition{}, err
	}
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "none" || raw == "" {
		return transition(0, "ease-out"), nil
	}
	t := Transition{Duration: -1}
	easing := ""
	for _, word := range strings.Fields(strings.ReplaceAll(raw, ", ", ",")) {
		if d, ok := parseDuration(word); ok && t.Duration < 0 {
			t.Duration = d
			continue
		}
		if easing != "" {
			return Transition{}, fmt.Errorf("%q: expected a duration and an easing, such as \"140ms ease-out\"", raw)
		}
		easing = word
	}
	if t.Duration < 0 {
		return Transition{}, fmt.Errorf("%q: expected a duration, such as 140ms", raw)
	}
	if easing == "" {
		easing = "ease"
	}
	return parseEasing(t, easing)
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
	if err != nil || v < 0 {
		return 0, false
	}
	return v * scale, true
}

// parseEasing sets t's easing: a named one or cubic-bezier(x1, y1, x2, y2).
func parseEasing(t Transition, easing string) (Transition, error) {
	easing = strings.ToLower(strings.TrimSpace(easing))
	if curve, ok := easings[easing]; ok {
		t.Easing, t.Curve = easing, curve
		return t, nil
	}
	args, ok := strings.CutPrefix(easing, "cubic-bezier(")
	if args, ok2 := strings.CutSuffix(args, ")"); ok && ok2 {
		var curve [4]float64
		parts := strings.Split(args, ",")
		if len(parts) == 4 {
			valid := true
			for i, part := range parts {
				v, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
				valid = valid && err == nil && (i%2 == 1 || v >= 0 && v <= 1)
				curve[i] = v
			}
			if valid {
				t.Curve = curve
				t.Easing = fmt.Sprintf("cubic-bezier(%s, %s, %s, %s)", formatNumber(curve[0]), formatNumber(curve[1]), formatNumber(curve[2]), formatNumber(curve[3]))
				return t, nil
			}
		}
	}
	return Transition{}, fmt.Errorf("unknown easing %q; expected linear, ease, ease-in, ease-out, ease-in-out or cubic-bezier(x1, y1, x2, y2) with x1 and x2 from 0 to 1", easing)
}

func formatTransition(t Transition) string {
	if t.Duration == 0 {
		return "none"
	}
	return formatNumber(t.Duration) + "ms " + t.Easing
}

// Ease is how far through its change a transition is at fraction p of
// its duration.
func (t Transition) Ease(p float64) float64 {
	if p <= 0 {
		return 0
	}
	if p >= 1 {
		return 1
	}
	x1, y1, x2, y2 := t.Curve[0], t.Curve[1], t.Curve[2], t.Curve[3]
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

// Scaled is how long t lasts at scale, in milliseconds.
func (t Transition) Scaled(scale float64) float64 {
	return math.Max(0, t.Duration*scale)
}

// transitionOption holds a transition, written as text or as a table with
// duration and easing.
func transitionOption(description string, field func(*Config) *Transition) optionDesc {
	return optionDesc{
		kind:        "transition",
		description: description,
		get:         func(L *lua.LState, cfg *Config) lua.LValue { return lua.LString(formatTransition(*field(cfg))) },
		format:      func(cfg *Config) string { return strconv.Quote(formatTransition(*field(cfg))) },
		applyText: func(cfg *Config, raw string) error {
			t, err := parseTransition(raw)
			if err != nil {
				return err
			}
			*field(cfg) = t
			return nil
		},
		apply: func(cfg *Config, value lua.LValue) error {
			switch value := value.(type) {
			case lua.LString:
				t, err := parseTransition(string(value))
				if err != nil {
					return err
				}
				*field(cfg) = t
				return nil
			case lua.LNumber:
				t := *field(cfg)
				t.Duration = max(0, float64(value))
				*field(cfg) = t
				return nil
			case *lua.LTable:
				t := *field(cfg)
				if d, ok := value.RawGetString("duration").(lua.LNumber); ok {
					t.Duration = max(0, float64(d))
				}
				if e, ok := value.RawGetString("easing").(lua.LString); ok {
					var err error
					if t, err = parseEasing(t, string(e)); err != nil {
						return err
					}
				}
				*field(cfg) = t
				return nil
			}
			return fmt.Errorf(`expected a transition such as "140ms ease-out" or { duration = 140, easing = "ease-out" }`)
		},
	}
}
