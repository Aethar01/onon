package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestElementStylesCascade(t *testing.T) {
	rt := mustLoadThemeTestConfig(t, `
onon.theme = {
  radius = 4,
  elements = {
    row = { text = "#102030", fill = "#eeeeee" },
    row_selected = { secondary = "accent" },
    panel = { radius = { 10, 10, 2, 2 } },
  },
}
`)
	theme := rt.Config().Theme
	selected := theme.Style(ElementRowSelected)
	// A row's override carries to the selected row, but the selected row's
	// own defaults outrank it, as a more specific rule would in CSS.
	if got := selected.Text.V; got != (Color{RGB: [3]uint8{0x10, 0x20, 0x30}, Alpha: 1}) {
		t.Errorf("row_selected text = %+v, want the row's", got)
	}
	if got := selected.Fill.V; got != paletteColor("accent", 0.1) {
		t.Errorf("row_selected fill = %+v, want its own default", got)
	}
	if selected.BorderSides.V != SideLeft || selected.BorderColor.V != paletteColor("accent", 1) {
		t.Errorf("row_selected border = %v %+v, want the accent ribbon at its left", selected.BorderSides.V, selected.BorderColor.V)
	}
	if got := selected.Secondary.V; got != paletteColor("accent", 1) {
		t.Errorf("row_selected secondary = %+v", got)
	}
	// The header follows its panel's top corners.
	if got := theme.Style(ElementHeader).Radius.V; got != (Corners{10, 10, 0, 0}) {
		t.Errorf("header radius = %v, want the panel's top corners", got)
	}
	// Completion starts from the panel.
	if got := theme.Style(ElementCompletion).Radius.V; got != (Corners{10, 10, 2, 2}) {
		t.Errorf("completion radius = %v, want the panel's", got)
	}
}

func TestEveryStyleSetsEveryProperty(t *testing.T) {
	for _, name := range ThemeNames() {
		theme := mustPreset(t, name)
		for e := range ElementCount {
			style := theme.Style(e)
			for _, p := range styleProps {
				if !p.isSet(&style) {
					t.Errorf("%s: %s.%s is unset", name, e, p.name)
				}
			}
		}
	}
}

func TestStatusBarStyleIsAPreset(t *testing.T) {
	theme := mustPreset(t, DefaultTheme)
	if status := theme.Style(ElementStatus); status.Floating.V || status.Fill.V.Name != "status_bar" || status.BorderSides.V != SideTop {
		t.Fatalf("bar status = %+v", status)
	}
	theme.StatusBarStyle = "pill"
	if status := theme.Style(ElementStatus); !status.Floating.V || status.Fill.V.Alpha != 0 || status.Margin.V != uniform(10) {
		t.Fatalf("pill status = %+v", status)
	}
	if left := theme.Style(ElementStatusLeft); left.Shape.V.Kind != "pill" || left.Fill.V.Name != "status_bar" {
		t.Fatalf("pill status_left = %+v", left)
	}
	if right := theme.Style(ElementStatusRight); right.Shape.V.Kind != "pill" || right.Text.V.Name != "muted" {
		t.Fatalf("status_right should start from status_left: %+v", right)
	}
	// What the theme sets goes over the preset.
	theme.Elements[ElementStatus].Floating = some(false)
	if theme.Style(ElementStatus).Floating.V {
		t.Fatal("status.floating = false did not override the pill preset")
	}
	theme.Radius = 0
	if left := theme.Style(ElementStatusLeft); left.Shape.V.Kind != "rect" {
		t.Fatalf("square theme pill shape = %+v", left.Shape.V)
	}
}

func TestElementPropertiesParse(t *testing.T) {
	rt := mustLoadThemeTestConfig(t, `
local e = onon.theme.elements
e.panel.fill = "accent/16"
e.panel.border = { width = 2, color = "#11223344", sides = { "top", "left" } }
e.panel.shadow = "0 3 8 shadow, 1 2 3 4 #000000"
e.panel.padding = { 1, 2 }
e.header.padding = { 1, 2, 3, 4 }
e.hint.radius = { 1, 2, 3, 4 }
e.hint.shadow = { { y = 2, blur = 4 }, { x = 1, color = "accent" } }
e.hint.fill = "none"
e.row.shape = "M0,0 H100% V100%-4 L50%,100% L0,100%-4 Z"
e.cursor.width = 2
e.status.opacity = 5
e.status_left.border = false
assert(e.panel.fill == "accent/16", e.panel.fill)
assert(e.panel.border.width == 2)
assert(e.panel.border.sides == "top left", e.panel.border.sides)
assert(e.panel.shadow[2].spread == 4)
assert(e.panel.padding[2] == 2)
assert(e.status.opacity == 1)
`)
	theme := rt.Config().Theme
	panel := theme.Style(ElementPanel)
	want := Shadow{N: 2, Layers: [maxShadowLayers]ShadowLayer{
		{Y: 3, Blur: 8, Color: Color{Name: "shadow", Alpha: 1}},
		{X: 1, Y: 2, Blur: 3, Spread: 4, Color: Color{Alpha: 1}},
	}}
	if panel.Shadow.V != want {
		t.Errorf("panel shadow = %+v", panel.Shadow.V)
	}
	if panel.BorderColor.V != (Color{RGB: [3]uint8{0x11, 0x22, 0x33}, Alpha: float64(0x44) / 255}) || panel.BorderSides.V != SideTop|SideLeft {
		t.Errorf("panel border = %+v %v", panel.BorderColor.V, panel.BorderSides.V)
	}
	if got := theme.Style(ElementHeader).Padding.V; got != (Insets{1, 2, 3, 4}) {
		t.Errorf("header padding = %+v", got)
	}
	hint := theme.Style(ElementHint)
	if hint.Radius.V != (Corners{1, 2, 3, 4}) || hint.Fill.V.Alpha != 0 || hint.Shadow.V.N != 2 || hint.Shadow.V.Layers[1].Color.Name != "accent" {
		t.Errorf("hint = %+v", hint)
	}
	if got := theme.Style(ElementRow).Shape.V; got.Kind != "path" || !strings.HasPrefix(got.Path, "M0,0") {
		t.Errorf("row shape = %+v", got)
	}
	if theme.Style(ElementStatusLeft).BorderWidth.V != 0 {
		t.Error("border = false left a border")
	}
}

func TestElementErrors(t *testing.T) {
	tests := []struct {
		name, lua, want string
	}{
		{"unknown colour", `onon.theme.elements.panel.fill = "acent"`, `unknown colour "acent"`},
		{"bad opacity", `onon.theme.elements.panel.fill = "accent/150"`, `expected a percentage`},
		{"negative radius", `onon.theme.elements.panel.radius = -1`, `must not be negative`},
		{"negative padding", `onon.theme.elements.row.padding = { 2, -6.5 }`, `must not be negative`},
		{"three radii", `onon.theme.elements.panel.radius = { 1, 2, 3 }`, `one radius or four`},
		{"bad side", `onon.theme.elements.panel.border = { sides = "middle" }`, `unknown side "middle"`},
		{"bad path", `onon.theme.elements.panel.shape = "M 0"`, `path ends where a coordinate was expected`},
		{"bad shadow", `onon.theme.elements.panel.shadow = "3"`, `expected x y`},
		{"shadow field", `onon.theme.elements.panel.shadow = { blurr = 3 }`, `unknown shadow field "blurr"`},
		{"elements not a table", `onon.theme.elements = 3`, `expected a table of elements`},
		{"read unknown element", `local _ = onon.theme.elements.nope`, `unknown element`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadThemeTestConfig(t, tt.lua)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestElementOptionsAtRuntime(t *testing.T) {
	rt := mustLoadThemeTestConfig(t, `onon.theme.radius = 6`)
	if got, err := rt.OptionValue("theme.elements.panel.radius"); err != nil || got != "6" {
		t.Fatalf("panel radius = %q, %v; want the theme's radius in effect", got, err)
	}
	if err := rt.SetOption("theme.elements.panel.radius", "1,2,3,4"); err != nil {
		t.Fatal(err)
	}
	if err := rt.SetOption("theme.elements.panel.border.color", "accent/50"); err != nil {
		t.Fatal(err)
	}
	if err := rt.SetOption("theme.elements.status.shadow", `"0 1 2 shadow"`); err != nil {
		t.Fatal(err)
	}
	if err := rt.ToggleOption("theme.elements.status.floating"); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"theme.elements.panel.radius":       "{ 1, 2, 3, 4 }",
		"theme.elements.panel.border.color": `"accent/50"`,
		"theme.elements.status.shadow":      `"0 1 2 0 shadow"`,
		"theme.elements.status.floating":    "true",
	} {
		if got, _ := rt.OptionValue(name); got != want {
			t.Errorf("%s = %s, want %s", name, got, want)
		}
	}
	if err := rt.SetOption("theme.elements.prompt.radius", "3"); err == nil {
		t.Error("expected prompt.radius to be refused")
	}
	// Element properties stay out of the option list.
	for _, name := range rt.OptionNames() {
		if strings.HasPrefix(name, elementsOptionPrefix) {
			t.Fatalf("option list includes %s", name)
		}
	}
	// Assigning nil clears an override, so the theme's value shows again.
	if _, err := rt.Eval(`onon.theme.elements.panel.radius = nil`); err != nil {
		t.Fatal(err)
	}
	if got, _ := rt.OptionValue("theme.elements.panel.radius"); got != "6" {
		t.Fatalf("cleared panel radius = %s", got)
	}
}

func TestElementsTableRoundTrips(t *testing.T) {
	rt := mustLoadThemeTestConfig(t, `
onon.theme = {
  base = "classic",
  elements = {
    panel = { radius = { 1, 2, 3, 4 }, border = { color = "accent/25" }, shadow = true },
    hint = { shape = function(path, w, h) path:rect(0, 0, w, h, 3) end, opacity = 0.5 },
    status = { floating = true, margin = { 1, 2 } },
  },
}
local copy = onon.theme
assert(copy.elements.hint.opacity == 0.5)
onon.theme = onon.options.theme
`)
	theme := rt.Config().Theme
	cfg := Default()
	got, err := themeFromLua(&cfg, luaThemeTable(rt.state, theme))
	if err != nil {
		t.Fatal(err)
	}
	if got != theme {
		t.Fatalf("theme did not survive a trip through Lua:\n%+v\nwant %+v", got.Elements, theme.Elements)
	}
	if theme.Elements[ElementHint].Shape.V.Func == nil {
		t.Fatal("the hint's shape function was lost")
	}
	// A preset sets no element overrides, so its plain table has none.
	if tbl := luaThemeTable(rt.state, mustPreset(t, "birch")).RawGetString("elements").(*lua.LTable); tbl.Len() != 0 {
		t.Fatal("a preset's elements table should be empty")
	}
	if key, _ := luaThemeTable(rt.state, mustPreset(t, "birch")).RawGetString("elements").(*lua.LTable).Next(lua.LNil); key != lua.LNil {
		t.Fatalf("a preset's elements table holds %v", key)
	}
}

func TestParsePath(t *testing.T) {
	ops, err := ParsePath("M 0,0 h 100%-6 v10 L 50% 100%+2 q1 2 3 4 C 1,2 3,4 5,6 z")
	if err != nil {
		t.Fatal(err)
	}
	px := func(v float64) Length { return Length{Px: v} }
	want := []PathOp{
		{Op: 'M', Pts: [3]PathPoint{{px(0), px(0)}}},
		{Op: 'L', Pts: [3]PathPoint{{Length{1, -6}, px(0)}}},
		{Op: 'L', Pts: [3]PathPoint{{Length{1, -6}, px(10)}}},
		{Op: 'L', Pts: [3]PathPoint{{Length{Frac: 0.5}, Length{1, 2}}}},
		{Op: 'Q', Pts: [3]PathPoint{{Length{0.5, 1}, Length{1, 4}}, {Length{0.5, 3}, Length{1, 6}}}},
		{Op: 'C', Pts: [3]PathPoint{{px(1), px(2)}, {px(3), px(4)}, {px(5), px(6)}}},
		{Op: 'Z'},
	}
	if len(ops) != len(want) {
		t.Fatalf("ops = %+v", ops)
	}
	for i := range want {
		if ops[i] != want[i] {
			t.Errorf("op %d = %+v, want %+v", i, ops[i], want[i])
		}
	}
	// Coordinates after a move are lines.
	if ops, err := ParsePath("M0 0 10 0 10 10"); err != nil || len(ops) != 3 || ops[2].Op != 'L' {
		t.Fatalf("implicit lines = %+v, %v", ops, err)
	}
	for _, bad := range []string{"", "L 1 1", "0 0", "M 1", "M 1 1 Z 3", "M 1 1 A 1 1 0 0 0 2 2", "M 1% 1%x"} {
		if _, err := ParsePath(bad); err == nil {
			t.Errorf("ParsePath(%q) succeeded", bad)
		}
	}
}

func TestShapePathRunsTheFunction(t *testing.T) {
	rt := mustLoadThemeTestConfig(t, `
onon.theme.elements.hint.shape = function(path, w, h)
  path:move_to(0, 0):line_to(w, 0):quad_to(w, h, 0, h):close()
  path:ellipse(w / 2, h / 2, 2)
end
onon.theme.elements.row.shape = function(path, w, h) path:line_to(1, 1) end
onon.theme.elements.panel.shape = function(path, w, h) end
`)
	theme := rt.Config().Theme
	ops, err := rt.ShapePath(theme.Elements[ElementHint].Shape.V.Func, 40, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 4+6 || ops[1].Pts[0].X.Px != 40 || ops[2].Pts[1].Y.Px != 20 || ops[4].Op != 'M' || ops[4].Pts[0].X.Px != 22 {
		t.Fatalf("ops = %+v", ops)
	}
	if _, err := rt.ShapePath(theme.Elements[ElementRow].Shape.V.Func, 40, 20); err == nil || !strings.Contains(err.Error(), "start the path with move_to") {
		t.Fatalf("line_to first: %v", err)
	}
	if _, err := rt.ShapePath(theme.Elements[ElementPanel].Shape.V.Func, 40, 20); err == nil || !strings.Contains(err.Error(), "drew no path") {
		t.Fatalf("empty shape: %v", err)
	}
}

func TestStyleColorsFormatAsParsed(t *testing.T) {
	for _, text := range []string{"none", "accent", "accent/16", "shadow/50", "#102030", "#10203080", "muted/56.5"} {
		c, err := parseStyleColor(text)
		if err != nil {
			t.Fatalf("%s: %v", text, err)
		}
		if got := formatStyleColor(c); got != text {
			t.Errorf("%s formats as %s", text, got)
		}
	}
}

func TestElementReferencesCoverEveryElement(t *testing.T) {
	refs := ElementReferences()
	if len(refs) != int(ElementCount) {
		t.Fatalf("%d references for %d elements", len(refs), ElementCount)
	}
	for _, ref := range refs {
		if ref.Description == "" || ref.Defaults == "" {
			t.Errorf("%s is undocumented: %+v", ref.Name, ref)
		}
		for _, prop := range ref.Properties {
			if _, ok := styleProperty(prop); !ok && prop != "border" {
				t.Errorf("%s takes unknown property %s", ref.Name, prop)
			}
		}
	}
	for _, ref := range StylePropertyReferences() {
		if ref.Description == "" {
			t.Errorf("property %s is undocumented", ref.Name)
		}
	}
}

// recordingCanvas notes what a draw function draws.
type recordingCanvas struct{ calls []string }

func (c *recordingCanvas) Default() { c.calls = append(c.calls, "default") }
func (c *recordingCanvas) Fill(path []PathOp, clr Color) {
	c.calls = append(c.calls, "fill "+FormatPath(path)+" "+formatStyleColor(clr))
}
func (c *recordingCanvas) Stroke(path []PathOp, clr Color, width float64) {
	c.calls = append(c.calls, "stroke "+FormatPath(path)+" "+formatStyleColor(clr)+" "+formatNumber(width))
}
func (c *recordingCanvas) Text(x, y float64, text string, clr Color, o TextOptions) (float64, float64) {
	c.calls = append(c.calls, "text "+text)
	return 42, 16
}
func (c *recordingCanvas) Measure(text string, o TextOptions) (float64, float64) { return 10, 16 }
func (c *recordingCanvas) Clip(x, y, w, h float64)                               { c.calls = append(c.calls, "clip") }
func (c *recordingCanvas) Unclip()                                               { c.calls = append(c.calls, "unclip") }
func (c *recordingCanvas) Image(path string, x, y, w, h float64) (float64, float64, error) {
	c.calls = append(c.calls, "image "+filepath.Base(path))
	return w, h, nil
}
func (c *recordingCanvas) Page(page int, x, y, w, h float64) (float64, float64) {
	c.calls = append(c.calls, "page "+formatNumber(float64(page)))
	return w, h
}
func (c *recordingCanvas) Input(id string, x, y, w float64, o InputOptions) float64 {
	c.calls = append(c.calls, "input "+id)
	return 20
}
func (c *recordingCanvas) Button(id string, x, y, w float64, label string) float64 {
	c.calls = append(c.calls, "button "+id)
	return 20
}
func (c *recordingCanvas) Checkbox(id string, x, y float64, label string, checked bool) (float64, float64) {
	c.calls = append(c.calls, "checkbox "+id)
	return 20, 20
}
func (c *recordingCanvas) Region(id lua.LValue, x, y, w, h float64) bool {
	c.calls = append(c.calls, "region "+id.String())
	return false
}

func TestDrawFunctionDrawsOnTheCanvas(t *testing.T) {
	rt := mustLoadThemeTestConfig(t, `
onon.theme.elements.row.draw = function(canvas, box, state)
  assert(state.element == "row_selected", state.element)
  assert(box.w == 100 and box.h == 20 and not state.alt)
  canvas:default()
  canvas:rect(0, 0, 3, box.h, "accent")
  canvas:fill("M0,0 L100%,0 L50%,100% Z", "#ff000080")
  canvas:stroke(canvas:path():move_to(0, 0):line_to(box.w, 0), "border", 2)
  local w, h = canvas:measure("hi")
  assert(w == 10 and h == 16)
  assert(canvas:text(0, 0, "hi", "muted") == 42)
end
`)
	theme := rt.Config().Theme
	// Rows' draw function carries to the selected row.
	style := theme.Style(ElementRowSelected)
	canvas := &recordingCanvas{}
	if err := rt.RunDraw(style.Draw.V, canvas, DrawState{Element: style.Element, W: 100, H: 20}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"default",
		"fill M 0,0 L 3,0 L 3,20 L 0,20 L 0,0 Z accent",
		"fill M 0,0 L 100%,0 L 50%,100% Z #ff000080",
		"stroke M 0,0 L 100,0 border 2",
		"text hi",
	}
	if strings.Join(canvas.calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls:\n%s\nwant:\n%s", strings.Join(canvas.calls, "\n"), strings.Join(want, "\n"))
	}
	if err := rt.SetOption("theme.elements.row.draw", "x"); err == nil || !strings.Contains(err.Error(), "set it in Lua") {
		t.Fatalf(":set of draw = %v", err)
	}
	if _, err := rt.Eval(`onon.theme.elements.row.draw = false`); err != nil {
		t.Fatal(err)
	}
	if theme := rt.Config().Theme; theme.Style(ElementRow).Draw.V != nil {
		t.Fatal("draw = false left the function")
	}
}

func TestDrawFunctionErrors(t *testing.T) {
	rt := mustLoadThemeTestConfig(t, `
onon.theme.elements.hint.draw = function(canvas) canvas:fill("M 0", "accent") end
onon.theme.elements.panel.draw = function(canvas) canvas:fill("M0,0 L1,1", "acent") end
`)
	theme := rt.Config().Theme
	for e, want := range map[Element]string{ElementHint: "path ends", ElementPanel: `unknown colour "acent"`} {
		if err := rt.RunDraw(theme.Style(e).Draw.V, &recordingCanvas{}, DrawState{Element: e}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: error = %v, want %q", e, err, want)
		}
	}
	if _, err := loadThemeTestConfig(t, `onon.theme.elements.hint.draw = 3`); err == nil || !strings.Contains(err.Error(), "expected a function") {
		t.Fatalf("draw = 3: %v", err)
	}
}

func TestFormatPathParsesBack(t *testing.T) {
	text := "M 0,0 L 100%-6,0 Q 50%,100%+2 0,10 C 1,2 3,4 25%,-1 Z"
	ops, err := ParsePath(text)
	if err != nil {
		t.Fatal(err)
	}
	if got := FormatPath(ops); got != text {
		t.Fatalf("FormatPath = %q, want %q", got, text)
	}
}

func TestKeptCanvasCannotDraw(t *testing.T) {
	rt := mustLoadThemeTestConfig(t, `
kept = nil
onon.theme.elements.row.draw = function(canvas) kept = canvas end
`)
	theme := rt.Config().Theme
	if err := rt.RunDraw(theme.Style(ElementRow).Draw.V, &recordingCanvas{}, DrawState{}); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Eval(`kept:default()`); err == nil || !strings.Contains(err.Error(), "expected a canvas drawing now") {
		t.Fatalf("drawing on a kept canvas: %v", err)
	}
}

// nestingCanvas runs inner when its default is drawn, as a view's canvas
// draws its rows, each of which may have a draw function of its own.
type nestingCanvas struct {
	recordingCanvas
	inner func()
}

func (c *nestingCanvas) Default() { c.inner() }

func TestDrawFunctionsNest(t *testing.T) {
	rt := mustLoadThemeTestConfig(t, `
onon.theme.elements.row.draw = function(canvas, box) canvas:rect(0, 0, box.w, 1, "accent") end
outer = function(canvas, box)
  canvas:default()
  assert(box.w == 300, "the inner draw changed the outer box: " .. box.w)
  canvas:text(0, 0, "after", "foreground")
end
`)
	theme := rt.Config().Theme
	row := theme.Style(ElementRow).Draw.V
	inner := &recordingCanvas{}
	outer := &nestingCanvas{}
	outer.inner = func() {
		if err := rt.RunDraw(row, inner, DrawState{Element: ElementRow, W: 50, H: 20}); err != nil {
			t.Fatal(err)
		}
	}
	fn, ok := rt.state.GetGlobal("outer").(*lua.LFunction)
	if !ok {
		t.Fatal("no outer function")
	}
	if err := rt.RunDraw(fn, outer, DrawState{Element: ElementPanel, W: 300, H: 200}); err != nil {
		t.Fatal(err)
	}
	if len(inner.calls) != 1 || len(outer.calls) != 1 || outer.calls[0] != "text after" {
		t.Fatalf("inner drew %q, outer %q", inner.calls, outer.calls)
	}
}

// A draw function runs for every element it styles on every frame.
func BenchmarkRunDraw(b *testing.B) {
	path := filepath.Join(b.TempDir(), "config.lua")
	source := `
onon.theme.elements.row.draw = function(canvas, box, state)
  canvas:default()
  canvas:rect(0, 5, 3, box.h - 10, "accent", 1.5)
  canvas:stroke("M0,100% H100%", "accent/50", 1)
end
`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		b.Fatal(err)
	}
	rt, err := OpenWithOptions(path, "", OpenOptions{NoPlugins: true})
	if err != nil {
		b.Fatal(err)
	}
	defer rt.Close()
	theme := rt.Config().Theme
	fn := theme.Style(ElementRow).Draw.V
	b.ReportAllocs()
	for b.Loop() {
		if err := rt.RunDraw(fn, &recordingCanvas{}, DrawState{Element: ElementRow, W: 300, H: 24}); err != nil {
			b.Fatal(err)
		}
	}
}
