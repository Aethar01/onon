package config

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

// Element is a piece of the UI drawn in a style of its own, which the
// theme's elements table can change.
type Element int

const (
	ElementPanel Element = iota
	ElementCompletion
	ElementHeader
	ElementRow
	ElementRowSelected
	ElementRowDisabled
	ElementHeading
	ElementButton
	ElementScrollbar
	ElementSwatch
	ElementStatus
	ElementStatusLeft
	ElementStatusRight
	ElementPrompt
	ElementCursor
	ElementInputSelection
	ElementInput
	ElementInputFocused
	ElementButtonFocused
	ElementCheck
	ElementHint
	ElementOverviewSelection
	ElementLinkPreview
	ElementTitleBar
	ElementTitleButton
	ElementTitleClose
	ElementLoader
	ElementAutoscrollMarker
	ElementPage
	ElementCount
)

const noElement Element = -1

// The properties an element can take: every element with a box takes
// boxProps, and those with text textProps.
var (
	boxProps  = []string{"shape", "radius", "fill", "border", "shadow", "opacity", "draw"}
	textProps = []string{"text", "secondary", "bold", "padding"}
)

var elementSpecs = [ElementCount]struct {
	name        string
	parent      Element // whose style the element starts from
	props       []string
	description string
}{
	ElementPanel:             {"panel", noElement, slices.Concat(boxProps, []string{"padding"}), "Menus and other floating panels. Its padding surrounds a menu's header and rows."},
	ElementCompletion:        {"completion", ElementPanel, slices.Concat(boxProps, []string{"padding"}), "The completion panel above the prompt. Its padding surrounds its rows."},
	ElementHeader:            {"header", noElement, slices.Concat(boxProps, textProps, []string{"gap"}), "A menu's title, a line of text with its padding around it. Its secondary colour is for the count or query after the title, set off by gap."},
	ElementRow:               {"row", noElement, slices.Concat(boxProps, textProps), "A row of a menu or of completion. Its secondary colour is for key bindings and the detail on the right. Its padding above and below sets the height of rows."},
	ElementRowSelected:       {"row_selected", ElementRow, slices.Concat(boxProps, textProps), "The selected row."},
	ElementRowDisabled:       {"row_disabled", ElementRow, slices.Concat(boxProps, textProps), "A row that cannot be chosen."},
	ElementHeading:           {"heading", ElementRow, slices.Concat(boxProps, textProps), "A section title among a menu's rows."},
	ElementButton:            {"button", ElementRow, slices.Concat(boxProps, textProps), "A button in a menu, such as the keybind menu's new keybind."},
	ElementScrollbar:         {"scrollbar", noElement, slices.Concat(boxProps, []string{"width", "margin"}), "The thumb of a menu's scrollbar; margin.right sets it off the panel's edge."},
	ElementSwatch:            {"swatch", noElement, slices.Concat(boxProps, []string{"gap"}), "A colour sample before a row's text, gap before the text."},
	ElementStatus:            {"status", noElement, slices.Concat(boxProps, []string{"margin", "gap", "floating"}), "The status bar as a whole: margin sets it off the window's edges, gap between its sides, and floating draws it over the page instead of below it."},
	ElementStatusLeft:        {"status_left", noElement, slices.Concat(boxProps, []string{"text", "padding"}), "The left of the status bar, holding messages and prompts. Its padding above and below sets the bar's height."},
	ElementStatusRight:       {"status_right", ElementStatusLeft, slices.Concat(boxProps, []string{"text", "padding"}), "The right of the status bar; the sides of its padding set its text in."},
	ElementPrompt:            {"prompt", noElement, []string{"text"}, "A prompt's prefix, such as : or /."},
	ElementCursor:            {"cursor", noElement, slices.Concat(boxProps, []string{"width"}), "The text cursor in a prompt."},
	ElementInputSelection:    {"input_selection", noElement, boxProps, "Selected text in a prompt."},
	ElementInput:             {"input", ElementRow, slices.Concat(boxProps, textProps), "A text field a plugin's view draws. Its secondary colour is for the placeholder."},
	ElementInputFocused:      {"input_focused", ElementInput, slices.Concat(boxProps, textProps), "The text field typing goes to."},
	ElementButtonFocused:     {"button_focused", ElementButton, slices.Concat(boxProps, textProps), "A button a plugin's view draws, under the pointer or with the keys, which Space or Enter clicks."},
	ElementCheck:             {"check", noElement, boxProps, "The mark in a checked check box, drawn in the box; a check box's box is an input."},
	ElementHint:              {"hint", noElement, slices.Concat(boxProps, textProps), "A link hint label. Its secondary colour is for the letters already typed."},
	ElementOverviewSelection: {"overview_selection", noElement, slices.Concat(boxProps, []string{"padding"}), "The outline around the overview's selected page; padding is its distance from the page."},
	ElementLinkPreview:       {"link_preview", noElement, []string{"radius", "fill", "border", "shadow", "opacity", "draw"}, "The popup previewing a link's target, a rect with corners rounded by its radius. Its fill shows while the page renders."},
	ElementTitleBar:          {"title_bar", noElement, slices.Concat(boxProps, []string{"text"}), "Behind the window buttons onon draws where the system draws no title bar, as on Windows. Its text colour draws the buttons' symbols."},
	ElementTitleButton:       {"title_button", noElement, slices.Concat(boxProps, []string{"text"}), "A window button under the pointer, drawn a little fainter while pressed."},
	ElementTitleClose:        {"title_close", ElementTitleButton, slices.Concat(boxProps, []string{"text"}), "The close button under the pointer."},
	ElementLoader:            {"loader", noElement, []string{"fill", "opacity", "draw"}, "The drops bouncing on a page still rendering; its box is the page."},
	ElementPage:              {"page", noElement, []string{"shape", "radius", "fill", "border", "shadow"}, "Each page of the document, its fill showing until the page renders. Pages turned other than by quarter turns are drawn plain."},
	ElementAutoscrollMarker:  {"autoscroll_marker", noElement, []string{"fill", "border", "opacity", "width", "draw"}, "The marker where autoscroll started: its fill, its outline as its border, and its width."},
}

func (e Element) String() string { return elementSpecs[e].name }

func elementNamed(name string) (Element, bool) {
	for e, spec := range elementSpecs {
		if spec.name == name {
			return Element(e), true
		}
	}
	return 0, false
}

// Opt is a style property, Set when the style gives it.
type Opt[T comparable] struct {
	V   T
	Set bool
}

func some[T comparable](v T) Opt[T] { return Opt[T]{V: v, Set: true} }

// Style is how an element is drawn. Properties a theme leaves unset come
// from the element's parent and defaults; the styles Theme.Style returns
// have every property set.
type Style struct {
	Shape       Opt[Shape]
	Radius      Opt[Corners]
	Fill        Opt[Color]
	BorderWidth Opt[float64]
	BorderColor Opt[Color]
	BorderSides Opt[Sides]
	Shadow      Opt[Shadow]
	Opacity     Opt[float64]
	Text        Opt[Color]
	Secondary   Opt[Color]
	Bold        Opt[bool]
	Padding     Opt[Insets]
	Margin      Opt[Insets]
	Width       Opt[float64]
	Gap         Opt[float64]
	Floating    Opt[bool]
	// Draw draws the element's box in place of the default drawing.
	Draw Opt[*lua.LFunction]

	// Element is the element Theme.Style resolved the style for.
	Element Element
}

// Shape is the outline of an element's box: a rect, its corners rounded
// by the radius; a pill, with round ends; or a path, given as text or
// drawn by a Lua function.
type Shape struct {
	Kind string         // rect, pill or path
	Path string         // SVG-like path data; see ParsePath
	Func *lua.LFunction // called as func(path, w, h) to draw the path
}

// Corners are radii clockwise from the top left.
type Corners [4]float64

// Insets are distances in from each side of a box.
type Insets struct{ Top, Right, Bottom, Left float64 }

// Sides is a set of a box's sides.
type Sides uint8

const (
	SideTop Sides = 1 << iota
	SideRight
	SideBottom
	SideLeft
	SidesAll = SideTop | SideRight | SideBottom | SideLeft
)

var sideNames = []struct {
	name string
	side Sides
}{{"top", SideTop}, {"right", SideRight}, {"bottom", SideBottom}, {"left", SideLeft}}

// Color is a style's colour: a palette colour by name, so that it follows
// the color mode, or a literal one, at an opacity from 0 to 1.
type Color struct {
	Name  string // a palette colour, or "shadow"; empty for RGB
	RGB   [3]uint8
	Alpha float64
}

// ShadowLayer is one shadow under a box, offset by X and Y, blurred like
// a CSS box-shadow, and grown by Spread before blurring.
type ShadowLayer struct {
	X, Y, Blur, Spread float64
	Color              Color
}

const maxShadowLayers = 4

// Shadow is the shadows under a box, the first drawn on top.
type Shadow struct {
	Layers [maxShadowLayers]ShadowLayer
	N      int
}

// List returns the shadow's layers.
func (s Shadow) List() []ShadowLayer { return s.Layers[:s.N] }

// defaultShadow is the shadow floating panels get when the theme's shadow
// is on: short and crisp, as one sheet of paper casts on another.
var defaultShadow = Shadow{N: 1, Layers: [maxShadowLayers]ShadowLayer{
	{Y: 2, Blur: 1, Color: Color{Name: "shadow", Alpha: 1}},
}}

func paletteColor(name string, alpha float64) Color { return Color{Name: name, Alpha: alpha} }

// baseStyle is where every style starts: an empty box with text in the
// UI colours.
func baseStyle() Style {
	return Style{
		Shape:       some(Shape{Kind: "rect"}),
		Radius:      some(Corners{}),
		Fill:        some(Color{}),
		BorderWidth: some(0.0),
		BorderColor: some(paletteColor("border", 1)),
		BorderSides: some(SidesAll),
		Shadow:      some(Shadow{}),
		Opacity:     some(1.0),
		Text:        some(paletteColor("foreground", 1)),
		Secondary:   some(paletteColor("muted", 1)),
		Bold:        some(false),
		Padding:     some(Insets{}),
		Margin:      some(Insets{}),
		Width:       some(1.0),
		Gap:         some(0.0),
		Floating:    some(false),
		Draw:        some[*lua.LFunction](nil),
	}
}

func uniform(v float64) Insets      { return Insets{v, v, v, v} }
func symmetric(v, h float64) Insets { return Insets{v, h, v, h} }

// elementDefaults is the style the theme's own fields give e, over its
// parent's style.
func elementDefaults(t *Theme, e Element) Style {
	radius, pad := float64(t.Radius), float64(t.Padding)
	line := max(4, pad)   // the padding above and below a line of text together
	textInset := 6 + line // text's distance from the sides of its panel
	shadow := Shadow{}
	if t.Shadow {
		shadow = defaultShadow
	}
	pill := t.StatusBarStyle == "pill"
	var s Style
	switch e {
	case ElementPanel:
		s.Radius = some(Corners{radius, radius, radius, radius})
		s.Fill = some(paletteColor("panel", 1))
		s.BorderWidth = some(1.0)
		s.Shadow = some(shadow)
		s.Padding = some(Insets{Bottom: line})
	case ElementCompletion:
		s.Padding = some(symmetric(line/2, 0))
	case ElementHeader:
		// The header's box spans the top of its panel, so it takes the
		// panel's top corners.
		panel := t.Style(ElementPanel).Radius.V
		s.Radius = some(Corners{panel[0], panel[1], 0, 0})
		s.Bold = some(true)
		s.BorderWidth = some(1.0)
		s.BorderSides = some(SideBottom)
		s.Padding = some(symmetric(line/2, textInset))
		s.Gap = some(8.0)
	case ElementRow:
		s.Padding = some(symmetric(line/2, textInset))
	case ElementRowSelected:
		// A flat band across the sheet, marked at its edge like a ribbon
		// bookmark.
		s.Fill = some(paletteColor("accent", 0.1))
		s.BorderWidth = some(3.0)
		s.BorderColor = some(paletteColor("accent", 1))
		s.BorderSides = some(SideLeft)
	case ElementRowDisabled:
		s.Text = some(paletteColor("muted", 1))
	case ElementHeading:
		s.Text = some(paletteColor("accent", 1))
		s.Bold = some(true)
	case ElementButton:
		s.Radius = some(corners(radius))
		s.BorderWidth = some(1.0)
		s.Text = some(paletteColor("accent", 1))
	case ElementScrollbar:
		s.Shape = some(Shape{Kind: "pill"})
		s.Fill = some(paletteColor("muted", 0.56))
		s.Width = some(4.0)
		s.Margin = some(Insets{Right: 5})
	case ElementSwatch:
		s.Radius = some(corners(radius / 2))
		s.BorderWidth = some(1.0)
		s.Gap = some(8.0)
	case ElementStatus:
		if pill {
			s.Margin = some(uniform(10))
			s.Gap = some(8.0)
			s.Floating = some(true)
		} else {
			s.Fill = some(paletteColor("status_bar", 1))
			s.BorderWidth = some(1.0)
			s.BorderSides = some(SideTop)
		}
	case ElementStatusLeft:
		s.Padding = some(symmetric(line/2, float64(t.StatusBarPadding)))
		if pill {
			if radius > 0 {
				s.Shape = some(Shape{Kind: "pill"})
			}
			s.Fill = some(paletteColor("status_bar", 1))
			s.BorderWidth = some(1.0)
			s.Shadow = some(shadow)
		}
	case ElementStatusRight:
		s.Text = some(paletteColor("muted", 1))
	case ElementPrompt:
		s.Text = some(paletteColor("accent", 1))
	case ElementCursor:
		s.Fill = some(paletteColor("accent", 1))
	case ElementInputSelection:
		s.Fill = some(paletteColor("accent", 0.3))
	case ElementInput:
		s.Radius = some(corners(radius / 2))
		s.Fill = some(paletteColor("background", 1))
		s.BorderWidth = some(1.0)
		s.Secondary = some(paletteColor("muted", 1))
		s.Padding = some(symmetric(line/2, 6))
	case ElementInputFocused:
		s.BorderColor = some(paletteColor("accent", 1))
	case ElementButtonFocused:
		s.Fill = some(paletteColor("accent", 0.1))
		s.BorderColor = some(paletteColor("accent", 1))
	case ElementCheck:
		s.Radius = some(corners(radius / 4))
		s.Fill = some(paletteColor("accent", 1))
	case ElementHint:
		// A solid tag pinned to the link.
		s.Radius = some(corners(radius / 2))
		s.Fill = some(paletteColor("accent", 1))
		s.Text = some(paletteColor("hint_foreground", 1))
		s.Secondary = some(paletteColor("hint_foreground", 0.55))
		s.Bold = some(true)
		s.Padding = some(symmetric(2, 5))
	case ElementOverviewSelection:
		s.Radius = some(corners(radius))
		s.BorderWidth = some(2.5)
		s.BorderColor = some(paletteColor("accent", 1))
		s.Padding = some(uniform(3))
	case ElementLinkPreview:
		s.Radius = some(corners(radius))
		s.Fill = some(paletteColor("page", 1))
		s.BorderWidth = some(1.0)
		s.Shadow = some(shadow)
	case ElementTitleBar:
		s.Fill = some(paletteColor("status_bar", 1))
	case ElementTitleButton:
		s.Fill = some(paletteColor("foreground", 0.15))
	case ElementTitleClose:
		// Windows' own close button red.
		s.Fill = some(Color{RGB: [3]uint8{0xc4, 0x2b, 0x1c}, Alpha: 1})
		s.Text = some(Color{RGB: [3]uint8{0xff, 0xff, 0xff}, Alpha: 1})
	case ElementLoader:
		s.Fill = some(paletteColor("muted", 1))
	case ElementPage:
		// Sheets on the desk, as the panels are.
		s.Fill = some(paletteColor("page", 1))
		s.Shadow = some(shadow)
	case ElementAutoscrollMarker:
		// Light with a dark edge, as a pointer is, to show on any page.
		s.Fill = some(Color{RGB: [3]uint8{250, 250, 250}, Alpha: 215.0 / 255})
		s.BorderWidth = some(1.2)
		s.BorderColor = some(Color{RGB: [3]uint8{40, 40, 40}, Alpha: 230.0 / 255})
		s.Width = some(32.0)
	}
	return s
}

func corners(r float64) Corners { return Corners{r, r, r, r} }

// Style is the style e is drawn in, with every property set: its parent's
// style, then e's defaults, then what the theme's elements table sets.
func (t *Theme) Style(e Element) Style {
	base := baseStyle()
	if parent := elementSpecs[e].parent; parent != noElement {
		base = t.Style(parent)
	}
	style := t.Elements[e].over(elementDefaults(t, e).over(base))
	style.Element = e
	return style
}

// over returns base with the properties s sets replaced.
func (s Style) over(base Style) Style {
	for _, p := range styleProps {
		p.overlay(&base, &s)
	}
	return base
}

// styleProp is a property of an element's style, as the elements table
// names it; border's are named border.width and so on.
type styleProp struct {
	name        string
	kind        string
	description string
	isSet       func(*Style) bool
	clear       func(*Style)
	overlay     func(dst, src *Style)
	apply       func(*Style, lua.LValue) error
	applyText   func(*Style, string) error
	toLua       func(*lua.LState, *Style) lua.LValue
	format      func(*Style) string
}

// optProp makes the property held in field, read from Lua by fromLua and
// from :set by fromText.
func optProp[T comparable](name, kind, description string, field func(*Style) *Opt[T],
	fromLua func(lua.LValue) (T, error), fromText func(string) (T, error),
	toLua func(*lua.LState, T) lua.LValue, format func(T) string) styleProp {
	return styleProp{
		name:        name,
		kind:        kind,
		description: description,
		isSet:       func(s *Style) bool { return field(s).Set },
		clear:       func(s *Style) { *field(s) = Opt[T]{} },
		overlay: func(dst, src *Style) {
			if field(src).Set {
				*field(dst) = *field(src)
			}
		},
		apply: func(s *Style, value lua.LValue) error {
			v, err := fromLua(value)
			if err != nil {
				return err
			}
			*field(s) = some(v)
			return nil
		},
		applyText: func(s *Style, raw string) error {
			v, err := fromText(strings.TrimSpace(raw))
			if err != nil {
				return err
			}
			*field(s) = some(v)
			return nil
		},
		toLua:  func(L *lua.LState, s *Style) lua.LValue { return toLua(L, field(s).V) },
		format: func(s *Style) string { return format(field(s).V) },
	}
}

var styleProps = []styleProp{
	optProp("shape", "shape", `"rect", with corners rounded by radius; "pill", with round ends; SVG-like path data; or a function(path, w, h) drawing the path. See [Shapes](#shapes).`,
		func(s *Style) *Opt[Shape] { return &s.Shape }, shapeFromLua, parseShape, shapeToLua, formatShape),
	optProp("radius", "corners", "Corner radius of a rect in logical pixels: one number, or { top_left, top_right, bottom_right, bottom_left }.",
		func(s *Style) *Opt[Corners] { return &s.Radius }, cornersFromLua, parseCorners, cornersToLua, formatCorners),
	colorProp("fill", "Colour of the box; \"none\" for no box.", func(s *Style) *Opt[Color] { return &s.Fill }),
	numberProp("border.width", "Width of the border inside the box's edge in logical pixels; 0 for none.", func(s *Style) *Opt[float64] { return &s.BorderWidth }),
	colorProp("border.color", "Colour of the border.", func(s *Style) *Opt[Color] { return &s.BorderColor }),
	optProp("border.sides", "sides", `Sides the border is drawn on: "all", or some of "top", "right", "bottom" and "left".`,
		func(s *Style) *Opt[Sides] { return &s.BorderSides }, sidesFromLua, parseSides, func(_ *lua.LState, v Sides) lua.LValue { return lua.LString(formatSides(v)) }, func(v Sides) string { return strconv.Quote(formatSides(v)) }),
	optProp("shadow", "shadow", `Shadows under the box: false; true for the default; { x, y, blur, spread, color }, or a list of them; or CSS box-shadow text such as "0 3 8 shadow, 0 1 2 shadow/50".`,
		func(s *Style) *Opt[Shadow] { return &s.Shadow }, shadowFromLua, parseShadow, shadowToLua, func(v Shadow) string { return strconv.Quote(formatShadow(v)) }),
	optProp("opacity", "number", "Opacity of the whole element, from 0 to 1.",
		func(s *Style) *Opt[float64] { return &s.Opacity }, opacityFromLua, func(raw string) (float64, error) { return opacityFromLua(numberText(raw)) }, numberToLua, formatNumber),
	colorProp("text", "Colour of the text.", func(s *Style) *Opt[Color] { return &s.Text }),
	colorProp("secondary", "Colour of secondary text, such as a row's detail.", func(s *Style) *Opt[Color] { return &s.Secondary }),
	optProp("bold", "boolean", "Draw the text in the heavier heading weight.",
		func(s *Style) *Opt[bool] { return &s.Bold }, boolFromLua, parseBoolOption, func(_ *lua.LState, v bool) lua.LValue { return lua.LBool(v) }, strconv.FormatBool),
	insetsProp("padding", "Space between the box's edges and its contents in logical pixels: one number, { vertical, horizontal }, or { top, right, bottom, left }.", func(s *Style) *Opt[Insets] { return &s.Padding }),
	insetsProp("margin", "Space around the box in logical pixels, written as padding is.", func(s *Style) *Opt[Insets] { return &s.Margin }),
	numberProp("width", "Width in logical pixels.", func(s *Style) *Opt[float64] { return &s.Width }),
	numberProp("gap", "Space between the element's parts in logical pixels.", func(s *Style) *Opt[float64] { return &s.Gap }),
	optProp("floating", "boolean", "Float over the page rather than take space from it.",
		func(s *Style) *Opt[bool] { return &s.Floating }, boolFromLua, parseBoolOption, func(_ *lua.LState, v bool) lua.LValue { return lua.LBool(v) }, strconv.FormatBool),
	optProp("draw", "function", "A function(canvas, box, state) that draws the box in place of the default drawing. See [Drawing](#drawing).",
		func(s *Style) *Opt[*lua.LFunction] { return &s.Draw }, drawFromLua,
		func(string) (*lua.LFunction, error) {
			return nil, fmt.Errorf("draw takes a Lua function; set it in Lua")
		},
		func(_ *lua.LState, fn *lua.LFunction) lua.LValue {
			if fn == nil {
				return lua.LNil
			}
			return fn
		},
		func(fn *lua.LFunction) string {
			if fn == nil {
				return "nil"
			}
			return "function"
		}),
}

func drawFromLua(value lua.LValue) (*lua.LFunction, error) {
	switch value := value.(type) {
	case *lua.LFunction:
		return value, nil
	case lua.LBool:
		if !value {
			return nil, nil // draw = false restores the default drawing
		}
	}
	return nil, fmt.Errorf("expected a function(canvas, box, state)")
}

// borderProps are the properties the border table holds.
var borderProps = []string{"width", "color", "sides"}

func styleProperty(name string) (styleProp, bool) {
	for _, p := range styleProps {
		if p.name == name {
			return p, true
		}
	}
	return styleProp{}, false
}

func colorProp(name, description string, field func(*Style) *Opt[Color]) styleProp {
	return optProp(name, "color", description, field, colorFromLua, parseStyleColor,
		func(_ *lua.LState, c Color) lua.LValue { return lua.LString(formatStyleColor(c)) },
		func(c Color) string { return strconv.Quote(formatStyleColor(c)) })
}

func numberProp(name, description string, field func(*Style) *Opt[float64]) styleProp {
	return optProp(name, "number", description, field, lengthFromLua, func(raw string) (float64, error) { return lengthFromLua(numberText(raw)) }, numberToLua, formatNumber)
}

func insetsProp(name, description string, field func(*Style) *Opt[Insets]) styleProp {
	return optProp(name, "insets", description, field, insetsFromLua, parseInsets, insetsToLua, formatInsets)
}

// numberText is raw as a Lua number, or as a string to be rejected.
func numberText(raw string) lua.LValue {
	if v, err := strconv.ParseFloat(raw, 64); err == nil {
		return lua.LNumber(v)
	}
	return lua.LString(raw)
}

func numberToLua(_ *lua.LState, v float64) lua.LValue { return lua.LNumber(v) }
func formatNumber(v float64) string                   { return strconv.FormatFloat(v, 'f', -1, 64) }

func lengthFromLua(value lua.LValue) (float64, error) {
	n, ok := value.(lua.LNumber)
	if !ok {
		return 0, fmt.Errorf("expected number")
	}
	if n < 0 {
		return 0, fmt.Errorf("must not be negative")
	}
	return float64(n), nil
}

func opacityFromLua(value lua.LValue) (float64, error) {
	n, ok := value.(lua.LNumber)
	if !ok {
		return 0, fmt.Errorf("expected number from 0 to 1")
	}
	return min(1, max(0, float64(n))), nil
}

func boolFromLua(value lua.LValue) (bool, error) {
	if value.Type() != lua.LTBool {
		return false, fmt.Errorf("expected boolean")
	}
	return lua.LVAsBool(value), nil
}

// numbersFromLua reads a number or a list of 1 to max numbers.
func numbersFromLua(value lua.LValue, maxLen int) ([]float64, error) {
	if n, ok := value.(lua.LNumber); ok {
		return []float64{float64(n)}, nil
	}
	tbl, ok := value.(*lua.LTable)
	if !ok || tbl.Len() == 0 || tbl.Len() > maxLen {
		return nil, fmt.Errorf("expected a number or a list of up to %d", maxLen)
	}
	var values []float64
	for i := 1; i <= tbl.Len(); i++ {
		n, ok := tbl.RawGetInt(i).(lua.LNumber)
		if !ok {
			return nil, fmt.Errorf("expected numbers")
		}
		values = append(values, float64(n))
	}
	return values, nil
}

// numbersText reads numbers separated by commas or spaces.
func numbersText(raw string) ([]float64, error) {
	var values []float64
	for _, field := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' || r == '{' || r == '}' }) {
		v, err := strconv.ParseFloat(field, 64)
		if err != nil {
			return nil, fmt.Errorf("expected numbers")
		}
		values = append(values, v)
	}
	return values, nil
}

func cornersFromLua(value lua.LValue) (Corners, error) {
	values, err := numbersFromLua(value, 4)
	if err != nil {
		return Corners{}, err
	}
	return cornersFrom(values)
}

func parseCorners(raw string) (Corners, error) {
	values, err := numbersText(raw)
	if err != nil {
		return Corners{}, err
	}
	return cornersFrom(values)
}

func cornersFrom(values []float64) (Corners, error) {
	if slices.ContainsFunc(values, func(v float64) bool { return v < 0 }) {
		return Corners{}, fmt.Errorf("radius must not be negative")
	}
	switch len(values) {
	case 1:
		return corners(values[0]), nil
	case 4:
		return Corners(values), nil
	}
	return Corners{}, fmt.Errorf("expected one radius or four")
}

func cornersToLua(L *lua.LState, c Corners) lua.LValue {
	if c == corners(c[0]) {
		return lua.LNumber(c[0])
	}
	return numberList(L, c[:]...)
}

func formatCorners(c Corners) string {
	if c == corners(c[0]) {
		return formatNumber(c[0])
	}
	return formatNumberList(c[:]...)
}

func insetsFromLua(value lua.LValue) (Insets, error) {
	values, err := numbersFromLua(value, 4)
	if err != nil {
		return Insets{}, err
	}
	return insetsFrom(values)
}

func parseInsets(raw string) (Insets, error) {
	values, err := numbersText(raw)
	if err != nil {
		return Insets{}, err
	}
	return insetsFrom(values)
}

func insetsFrom(values []float64) (Insets, error) {
	if slices.ContainsFunc(values, func(v float64) bool { return v < 0 }) {
		return Insets{}, fmt.Errorf("insets must not be negative")
	}
	switch len(values) {
	case 1:
		return uniform(values[0]), nil
	case 2:
		return symmetric(values[0], values[1]), nil
	case 4:
		return Insets{values[0], values[1], values[2], values[3]}, nil
	}
	return Insets{}, fmt.Errorf("expected one, two or four numbers")
}

func insetsToLua(L *lua.LState, in Insets) lua.LValue {
	switch {
	case in == uniform(in.Top):
		return lua.LNumber(in.Top)
	case in == symmetric(in.Top, in.Right):
		return numberList(L, in.Top, in.Right)
	}
	return numberList(L, in.Top, in.Right, in.Bottom, in.Left)
}

func formatInsets(in Insets) string {
	switch {
	case in == uniform(in.Top):
		return formatNumber(in.Top)
	case in == symmetric(in.Top, in.Right):
		return formatNumberList(in.Top, in.Right)
	}
	return formatNumberList(in.Top, in.Right, in.Bottom, in.Left)
}

func numberList(L *lua.LState, values ...float64) *lua.LTable {
	tbl := L.NewTable()
	for _, v := range values {
		tbl.Append(lua.LNumber(v))
	}
	return tbl
}

func formatNumberList(values ...float64) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = formatNumber(v)
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

func sidesFromLua(value lua.LValue) (Sides, error) {
	switch value := value.(type) {
	case lua.LString:
		return parseSides(string(value))
	case *lua.LTable:
		var words []string
		for i := 1; i <= value.Len(); i++ {
			words = append(words, lua.LVAsString(value.RawGetInt(i)))
		}
		return parseSides(strings.Join(words, " "))
	}
	return 0, fmt.Errorf(`expected "all" or sides such as "top bottom"`)
}

func parseSides(raw string) (Sides, error) {
	var sides Sides
	for _, word := range strings.FieldsFunc(strings.ToLower(raw), func(r rune) bool { return r == ',' || r == ' ' || r == '"' }) {
		if word == "all" {
			sides |= SidesAll
			continue
		}
		i := slices.IndexFunc(sideNames, func(s struct {
			name string
			side Sides
		}) bool {
			return s.name == word
		})
		if i < 0 {
			return 0, fmt.Errorf("unknown side %q; expected top, right, bottom, left or all", word)
		}
		sides |= sideNames[i].side
	}
	if sides == 0 {
		return 0, fmt.Errorf(`expected "all" or sides such as "top bottom"`)
	}
	return sides, nil
}

func formatSides(sides Sides) string {
	if sides == SidesAll {
		return "all"
	}
	var words []string
	for _, s := range sideNames {
		if sides&s.side != 0 {
			words = append(words, s.name)
		}
	}
	return strings.Join(words, " ")
}

func shapeFromLua(value lua.LValue) (Shape, error) {
	switch value := value.(type) {
	case lua.LString:
		return parseShape(string(value))
	case *lua.LFunction:
		return Shape{Kind: "path", Func: value}, nil
	}
	return Shape{}, fmt.Errorf(`expected "rect", "pill", path data or a function`)
}

func parseShape(raw string) (Shape, error) {
	raw, err := parseStringOption(raw)
	if err != nil {
		return Shape{}, err
	}
	switch kind := strings.ToLower(strings.TrimSpace(raw)); kind {
	case "rect", "pill":
		return Shape{Kind: kind}, nil
	}
	if _, err := ParsePath(raw); err != nil {
		return Shape{}, fmt.Errorf(`expected "rect", "pill" or path data: %w`, err)
	}
	return Shape{Kind: "path", Path: strings.TrimSpace(raw)}, nil
}

func shapeToLua(_ *lua.LState, s Shape) lua.LValue {
	switch {
	case s.Func != nil:
		return s.Func
	case s.Kind == "path":
		return lua.LString(s.Path)
	}
	return lua.LString(s.Kind)
}

func formatShape(s Shape) string {
	switch {
	case s.Func != nil:
		return "function"
	case s.Kind == "path":
		return strconv.Quote(s.Path)
	}
	return strconv.Quote(s.Kind)
}

// paletteNames are the colours a style can name: the palette's, and
// shadow, black at the strength shadows need over the background.
var paletteNames = func() []string {
	names := make([]string, 0, len(paletteColors)+1)
	for _, c := range paletteColors {
		names = append(names, c.name)
	}
	return append(names, "shadow")
}()

// Lookup returns the palette colour of that name.
func (p *Palette) Lookup(name string) ([3]uint8, bool) {
	for _, c := range paletteColors {
		if c.name == name {
			return *c.field(p), true
		}
	}
	return [3]uint8{}, false
}

func colorFromLua(value lua.LValue) (Color, error) {
	switch value := value.(type) {
	case lua.LString:
		return parseStyleColor(string(value))
	case *lua.LTable:
		return Color{RGB: readColor(value, [3]uint8{}), Alpha: 1}, nil
	}
	return Color{}, fmt.Errorf("expected a colour such as \"#RRGGBB\" or \"accent\"")
}

// parseStyleColor reads a style colour: "none", a palette colour's name,
// "#RRGGBB" or "#RRGGBBAA", optionally followed by /percent opacity, as
// "accent/16".
func parseStyleColor(raw string) (Color, error) {
	raw, err := parseStringOption(raw)
	if err != nil {
		return Color{}, err
	}
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "none" || raw == "transparent" {
		return Color{}, nil
	}
	alpha := 1.0
	if base, percent, ok := strings.Cut(raw, "/"); ok {
		p, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(percent), "%"), 64)
		if err != nil || p < 0 || p > 100 {
			return Color{}, fmt.Errorf("opacity %q: expected a percentage from 0 to 100", percent)
		}
		raw, alpha = strings.TrimSpace(base), p/100
	}
	if slices.Contains(paletteNames, raw) {
		return Color{Name: raw, Alpha: alpha}, nil
	}
	if strings.HasPrefix(raw, "#") && len(raw) == 9 {
		a, err := strconv.ParseUint(raw[7:], 16, 8)
		if err != nil {
			return Color{}, fmt.Errorf("expected #RRGGBBAA")
		}
		raw, alpha = raw[:7], alpha*float64(a)/255
	}
	rgb, err := parseColorOption(raw)
	if err != nil {
		return Color{}, fmt.Errorf("unknown colour %q; expected none, #RRGGBB, #RRGGBBAA or one of %s", raw, strings.Join(paletteNames, ", "))
	}
	return Color{RGB: rgb, Alpha: alpha}, nil
}

func formatStyleColor(c Color) string {
	switch {
	case c.Name == "" && c.Alpha == 0:
		return "none"
	case c.Name == "" && c.Alpha < 1:
		return fmt.Sprintf("%s%02x", FormatColor(c.RGB), uint8(math.Round(c.Alpha*255)))
	case c.Name == "":
		return FormatColor(c.RGB)
	case c.Alpha < 1:
		return c.Name + "/" + formatNumber(math.Round(c.Alpha*10000)/100)
	}
	return c.Name
}

func shadowFromLua(value lua.LValue) (Shadow, error) {
	switch value := value.(type) {
	case lua.LBool:
		if value {
			return defaultShadow, nil
		}
		return Shadow{}, nil
	case lua.LString:
		return parseShadow(string(value))
	case *lua.LTable:
		if value.Len() == 0 {
			layer, err := shadowLayerFromLua(value)
			return Shadow{N: 1, Layers: [maxShadowLayers]ShadowLayer{layer}}, err
		}
		if value.Len() > maxShadowLayers {
			return Shadow{}, fmt.Errorf("at most %d shadows", maxShadowLayers)
		}
		var s Shadow
		for i := 1; i <= value.Len(); i++ {
			tbl, ok := value.RawGetInt(i).(*lua.LTable)
			if !ok {
				return Shadow{}, fmt.Errorf("expected a list of shadow tables")
			}
			layer, err := shadowLayerFromLua(tbl)
			if err != nil {
				return Shadow{}, err
			}
			s.Layers[s.N] = layer
			s.N++
		}
		return s, nil
	}
	return Shadow{}, fmt.Errorf("expected false, true, a shadow table or a list of them")
}

func shadowLayerFromLua(tbl *lua.LTable) (ShadowLayer, error) {
	layer := ShadowLayer{Color: Color{Name: "shadow", Alpha: 1}}
	var err error
	tbl.ForEach(func(key, value lua.LValue) {
		if err != nil {
			return
		}
		name := strings.ToLower(lua.LVAsString(key))
		if name == "color" {
			layer.Color, err = colorFromLua(value)
			return
		}
		n, ok := value.(lua.LNumber)
		if !ok {
			err = fmt.Errorf("shadow %s: expected number", name)
			return
		}
		switch name {
		case "x":
			layer.X = float64(n)
		case "y":
			layer.Y = float64(n)
		case "blur":
			layer.Blur = max(0, float64(n))
		case "spread":
			layer.Spread = float64(n)
		default:
			err = fmt.Errorf("unknown shadow field %q; expected x, y, blur, spread or color", name)
		}
	})
	return layer, err
}

// parseShadow reads shadows written as in CSS box-shadow: "x y [blur
// [spread]] [color]", separated by commas, or none.
func parseShadow(raw string) (Shadow, error) {
	raw, err := parseStringOption(raw)
	if err != nil {
		return Shadow{}, err
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "none", "false", "":
		return Shadow{}, nil
	case "true", "default":
		return defaultShadow, nil
	}
	var s Shadow
	for _, part := range strings.Split(raw, ",") {
		if s.N == maxShadowLayers {
			return Shadow{}, fmt.Errorf("at most %d shadows", maxShadowLayers)
		}
		layer := ShadowLayer{Color: Color{Name: "shadow", Alpha: 1}}
		var numbers []float64
		for _, word := range strings.Fields(part) {
			if v, err := strconv.ParseFloat(strings.TrimSuffix(word, "px"), 64); err == nil {
				numbers = append(numbers, v)
				continue
			}
			if layer.Color, err = parseStyleColor(word); err != nil {
				return Shadow{}, err
			}
		}
		if len(numbers) < 2 || len(numbers) > 4 {
			return Shadow{}, fmt.Errorf("shadow %q: expected x y [blur [spread]] [color]", strings.TrimSpace(part))
		}
		numbers = append(numbers, 0, 0)
		layer.X, layer.Y, layer.Blur, layer.Spread = numbers[0], numbers[1], max(0, numbers[2]), numbers[3]
		s.Layers[s.N] = layer
		s.N++
	}
	return s, nil
}

func formatShadow(s Shadow) string {
	if s.N == 0 {
		return "none"
	}
	parts := make([]string, s.N)
	for i, l := range s.List() {
		parts[i] = strings.Join([]string{formatNumber(l.X), formatNumber(l.Y), formatNumber(l.Blur), formatNumber(l.Spread), formatStyleColor(l.Color)}, " ")
	}
	return strings.Join(parts, ", ")
}

func shadowToLua(L *lua.LState, s Shadow) lua.LValue {
	if s.N == 0 {
		return lua.LFalse
	}
	list := L.NewTable()
	for _, l := range s.List() {
		layer := L.NewTable()
		layer.RawSetString("x", lua.LNumber(l.X))
		layer.RawSetString("y", lua.LNumber(l.Y))
		layer.RawSetString("blur", lua.LNumber(l.Blur))
		layer.RawSetString("spread", lua.LNumber(l.Spread))
		layer.RawSetString("color", lua.LString(formatStyleColor(l.Color)))
		list.Append(layer)
	}
	return list
}

// elementsOptionPrefix names element properties as options, as in
// theme.elements.panel.radius.
const elementsOptionPrefix = themeOptionPrefix + "elements."

// elementProperty splits a name such as "panel.border.width" into its
// element and property, checking that the element takes it.
func elementProperty(name string) (Element, styleProp, error) {
	elementName, propName, _ := strings.Cut(name, ".")
	e, ok := elementNamed(elementName)
	if !ok {
		return 0, styleProp{}, fmt.Errorf("unknown element %q", elementName)
	}
	p, ok := styleProperty(propName)
	group, _, _ := strings.Cut(propName, ".")
	if !ok || !slices.Contains(elementSpecs[e].props, group) {
		return 0, styleProp{}, fmt.Errorf("%s has no property %q; it takes %s", e, propName, strings.Join(elementSpecs[e].props, ", "))
	}
	return e, p, nil
}

// elementOption is the option for an element's property, such as
// theme.elements.panel.radius. They are looked up by name rather than
// registered, so they stay out of option lists. Reading one gives the
// value in effect; assigning nil clears it.
func elementOption(name string) (optionDesc, bool) {
	rest, ok := strings.CutPrefix(name, elementsOptionPrefix)
	if !ok {
		return optionDesc{}, false
	}
	e, p, err := elementProperty(rest)
	if err != nil {
		return optionDesc{}, false
	}
	return optionDesc{
		kind:        p.kind,
		description: p.description,
		get: func(L *lua.LState, cfg *Config) lua.LValue {
			style := cfg.Theme.Style(e)
			return p.toLua(L, &style)
		},
		format: func(cfg *Config) string {
			style := cfg.Theme.Style(e)
			return p.format(&style)
		},
		applyText: func(cfg *Config, raw string) error {
			return p.applyText(&cfg.Theme.Elements[e], raw)
		},
		apply: func(cfg *Config, value lua.LValue) error {
			if value == lua.LNil {
				p.clear(&cfg.Theme.Elements[e])
				return nil
			}
			return p.apply(&cfg.Theme.Elements[e], value)
		},
	}, true
}

// lookupOption finds a registered option, or an element's property.
func lookupOption(name string) (optionDesc, bool) {
	if desc, ok := configOptions[name]; ok {
		return desc, true
	}
	return elementOption(name)
}

// applyElementsTable sets what tbl, as the theme's elements table, holds.
func applyElementsTable(cfg *Config, tbl *lua.LTable) error {
	var skipped skips
	var err error
	tbl.ForEach(func(key, value lua.LValue) {
		if err == nil {
			err = skipped.add(applyElementValue(cfg, strings.ToLower(lua.LVAsString(key)), value))
		}
	})
	if err != nil {
		return err
	}
	return skipped.err()
}

// applyElementValue sets name, an element, one of its properties or the
// border table, to value; a table sets the properties it holds.
func applyElementValue(cfg *Config, name string, value lua.LValue) error {
	elementName, propName, hasProp := strings.Cut(name, ".")
	if _, ok := elementNamed(elementName); !ok {
		return skipField("elements." + elementName)
	}
	if !hasProp || propName == "border" {
		switch value := value.(type) {
		case *lua.LTable:
			var skipped skips
			var err error
			value.ForEach(func(key, v lua.LValue) {
				if err == nil {
					err = skipped.add(applyElementValue(cfg, name+"."+strings.ToLower(lua.LVAsString(key)), v))
				}
			})
			if err != nil {
				return err
			}
			return skipped.err()
		case lua.LNumber:
			if hasProp { // border = 2 sets its width
				return applyElementValue(cfg, name+".width", value)
			}
		case lua.LBool:
			if hasProp && !bool(value) { // border = false removes it
				return applyElementValue(cfg, name+".width", lua.LNumber(0))
			}
		}
		if hasProp {
			return fmt.Errorf("elements.%s: expected a number, false or a table of %s", name, strings.Join(borderProps, ", "))
		}
		return fmt.Errorf("elements.%s: expected a table of properties", name)
	}
	desc, ok := elementOption(elementsOptionPrefix + name)
	if !ok {
		return skipField("elements." + name) // a property it does not take, or one unknown
	}
	if err := desc.apply(cfg, value); err != nil {
		return fmt.Errorf("elements.%s: %w", name, err)
	}
	return nil
}

// luaElementsTable writes the properties the elements set as plain tables,
// leaving out the rest.
func luaElementsTable(L *lua.LState, elements *[ElementCount]Style) *lua.LTable {
	tbl := L.NewTable()
	for e := range ElementCount {
		style := &elements[e]
		element := L.NewTable()
		for _, p := range styleProps {
			if !p.isSet(style) {
				continue
			}
			target := element
			name := p.name
			if member, ok := strings.CutPrefix(name, "border."); ok {
				border, ok := element.RawGetString("border").(*lua.LTable)
				if !ok {
					border = L.NewTable()
					element.RawSetString("border", border)
				}
				target, name = border, member
			}
			target.RawSetString(name, p.toLua(L, style))
		}
		if key, _ := element.Next(lua.LNil); key != lua.LNil {
			tbl.RawSetString(e.String(), element)
		}
	}
	return tbl
}

// newLuaElementsTable is onon.theme.elements, or with prefix one element
// or its border: reading a property gives the value in effect, and
// assigning one sets it.
func newLuaElementsTable(L *lua.LState, rt *Runtime, cfg *Config, prefix string) *lua.LTable {
	tbl := L.NewTable()
	mt := L.NewTable()
	L.SetField(mt, "__index", L.NewFunction(func(L *lua.LState) int {
		name := prefix + strings.ToLower(strings.TrimSpace(L.CheckString(2)))
		elementName, propName, hasProp := strings.Cut(name, ".")
		if _, ok := elementNamed(elementName); !ok {
			L.RaiseError("onon.theme.elements.%s: unknown element", elementName)
		}
		if !hasProp || propName == "border" {
			L.Push(newLuaElementsTable(L, rt, cfg, name+"."))
			return 1
		}
		desc, ok := elementOption(elementsOptionPrefix + name)
		if !ok {
			_, _, err := elementProperty(name)
			L.RaiseError("onon.theme.elements.%s: %v", name, err)
		}
		L.Push(desc.get(L, cfg))
		return 1
	}))
	L.SetField(mt, "__newindex", L.NewFunction(func(L *lua.LState) int {
		name := prefix + strings.ToLower(strings.TrimSpace(L.CheckString(2)))
		if err := rt.setThemeField("elements."+name, L.CheckAny(3)); err != nil {
			L.RaiseError("onon.theme.%v", err)
		}
		return 0
	}))
	L.SetMetatable(tbl, mt)
	return tbl
}

// ElementNames lists the elements in the order they are documented.
func ElementNames() []string {
	names := make([]string, ElementCount)
	for e := range ElementCount {
		names[e] = e.String()
	}
	return names
}

// ElementReference documents an element.
type ElementReference struct {
	Name        string
	Inherits    string // the element whose style it starts from, if any
	Description string
	Properties  []string
	Defaults    string // its style in the default theme, as Lua
}

// ElementReferences documents the elements with their styles in the
// default theme.
func ElementReferences() []ElementReference {
	theme := Default().Theme
	refs := make([]ElementReference, ElementCount)
	for e := range ElementCount {
		spec := elementSpecs[e]
		style := theme.Style(e)
		ref := ElementReference{Name: spec.name, Description: spec.description, Properties: spec.props, Defaults: formatStyle(&style, spec.props, true)}
		if spec.parent != noElement {
			ref.Inherits = spec.parent.String()
		}
		refs[e] = ref
	}
	return refs
}

// BaseStyleReference is the style every element starts from, as Lua.
func BaseStyleReference() string {
	style := baseStyle()
	var props []string
	for _, p := range styleProps {
		group, _, _ := strings.Cut(p.name, ".")
		if !slices.Contains(props, group) {
			props = append(props, group)
		}
	}
	return formatStyle(&style, props, false)
}

// formatStyle writes the properties of style that props names as Lua,
// with changed leaving out those the base style has.
func formatStyle(style *Style, props []string, changed bool) string {
	base := baseStyle()
	var fields, border []string
	for _, p := range styleProps {
		group, member, nested := strings.Cut(p.name, ".")
		if !slices.Contains(props, group) || changed && p.format(style) == p.format(&base) {
			continue
		}
		if nested {
			border = append(border, member+" = "+p.format(style))
			continue
		}
		if len(border) > 0 {
			fields = append(fields, "border = { "+strings.Join(border, ", ")+" }")
			border = nil
		}
		fields = append(fields, p.name+" = "+p.format(style))
	}
	if len(border) > 0 {
		fields = append(fields, "border = { "+strings.Join(border, ", ")+" }")
	}
	return strings.Join(fields, ", ")
}

// StylePropertyReference documents a style property.
type StylePropertyReference struct {
	Name, Type, Description string
}

// StylePropertyReferences documents the properties elements take.
func StylePropertyReferences() []StylePropertyReference {
	refs := make([]StylePropertyReference, len(styleProps))
	for i, p := range styleProps {
		refs[i] = StylePropertyReference{Name: p.name, Type: p.kind, Description: p.description}
	}
	return refs
}
