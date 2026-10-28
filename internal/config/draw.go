package config

import (
	"errors"
	"fmt"
	"path/filepath"

	lua "github.com/yuin/gopher-lua"
)

// luaPath is a path Lua builds, for a shape or for a canvas to draw.
type luaPath struct{ ops []PathOp }

const luaPathType = "onon.path"

var errNoMove = errors.New("start the path with move_to")

// newLuaPath makes an empty path. Its methods, each returning the path,
// are move_to(x, y), line_to(x, y), quad_to(cx, cy, x, y), cubic_to(c1x,
// c1y, c2x, c2y, x, y), close(), rect(x, y, w, h [, radius]) and
// ellipse(cx, cy, rx [, ry]).
func newLuaPath(L *lua.LState) (*lua.LUserData, *luaPath) {
	path := &luaPath{}
	ud := L.NewUserData()
	ud.Value = path
	L.SetMetatable(ud, luaPathMetatable(L))
	return ud, path
}

func luaPathMetatable(L *lua.LState) *lua.LTable {
	mt := L.NewTypeMetatable(luaPathType)
	if mt.RawGetString("__index") != lua.LNil {
		return mt
	}
	methods := L.NewTable()
	method := func(name string, required, optional int, add func(p *luaPath, v []float64) error) {
		L.SetField(methods, name, L.NewFunction(func(L *lua.LState) int {
			p := checkLuaPath(L, 1)
			v := make([]float64, required+optional)
			for i := range v {
				if i >= required && L.Get(i+2) == lua.LNil {
					v = v[:i]
					break
				}
				v[i] = float64(L.CheckNumber(i + 2))
			}
			if err := add(p, v); err != nil {
				L.RaiseError("path:%s: %v", name, err)
			}
			L.Push(L.Get(1))
			return 1
		}))
	}
	// add appends op once the path has started.
	add := func(p *luaPath, op PathOp) error {
		if len(p.ops) == 0 {
			return errNoMove
		}
		p.ops = append(p.ops, op)
		return nil
	}
	method("move_to", 2, 0, func(p *luaPath, v []float64) error {
		p.ops = append(p.ops, PathOp{Op: 'M', Pts: [3]PathPoint{pxPoint(v[0], v[1])}})
		return nil
	})
	method("line_to", 2, 0, func(p *luaPath, v []float64) error {
		return add(p, PathOp{Op: 'L', Pts: [3]PathPoint{pxPoint(v[0], v[1])}})
	})
	method("quad_to", 4, 0, func(p *luaPath, v []float64) error {
		return add(p, PathOp{Op: 'Q', Pts: [3]PathPoint{pxPoint(v[0], v[1]), pxPoint(v[2], v[3])}})
	})
	method("cubic_to", 6, 0, func(p *luaPath, v []float64) error {
		return add(p, PathOp{Op: 'C', Pts: [3]PathPoint{pxPoint(v[0], v[1]), pxPoint(v[2], v[3]), pxPoint(v[4], v[5])}})
	})
	method("close", 0, 0, func(p *luaPath, _ []float64) error { return add(p, PathOp{Op: 'Z'}) })
	method("rect", 4, 1, func(p *luaPath, v []float64) error {
		radius := 0.0
		if len(v) == 5 {
			radius = min(max(0, v[4]), v[2]/2, v[3]/2)
		}
		p.ops = append(p.ops, RoundedRectPath(v[0], v[1], v[2], v[3], corners(radius))...)
		return nil
	})
	method("ellipse", 3, 1, func(p *luaPath, v []float64) error {
		ry := v[2]
		if len(v) == 4 {
			ry = v[3]
		}
		p.ops = append(p.ops, ellipsePath(v[0], v[1], v[2], ry)...)
		return nil
	})
	L.SetField(mt, "__index", methods)
	return mt
}

func checkLuaPath(L *lua.LState, n int) *luaPath {
	if ud, ok := L.Get(n).(*lua.LUserData); ok {
		if p, ok := ud.Value.(*luaPath); ok {
			return p
		}
	}
	L.ArgError(n, "expected a path")
	return nil
}

// ShapePath runs a shape's function for a box w by h logical pixels and
// returns the path it drew. The function is called as fn(path, w, h), path
// being a path as newLuaPath makes.
func (r *Runtime) ShapePath(fn *lua.LFunction, w, h float64) ([]PathOp, error) {
	if r == nil || r.state == nil {
		return nil, fmt.Errorf("no Lua state")
	}
	ud, path := newLuaPath(r.state)
	if err := r.callLua(lua.P{Fn: fn, NRet: 0, Protect: true}, ud, lua.LNumber(w), lua.LNumber(h)); err != nil {
		return nil, err
	}
	if len(path.ops) == 0 {
		return nil, fmt.Errorf("the shape function drew no path")
	}
	return path.ops, nil
}

// Canvas is what an element's draw function draws on. Coordinates are in
// logical pixels from the top left of the element's box.
type Canvas interface {
	// Default draws the element as it would be without the function.
	Default()
	Fill(path []PathOp, c Color)
	Stroke(path []PathOp, c Color, width float64)
	// Text draws text with the top of its first line at y, returning the
	// size it takes.
	Text(x, y float64, text string, c Color, opts TextOptions) (w, h float64)
	Measure(text string, opts TextOptions) (w, h float64)
	// Clip limits drawing to a rect, within the clip the canvas was given,
	// until Unclip.
	Clip(x, y, w, h float64)
	Unclip()
	// Image draws the image file at path over a rect, a width or height
	// of 0 keeping its proportions, returning the size it took.
	Image(path string, x, y, w, h float64) (float64, float64, error)
	// Page draws a page (0-based) fitted to a rect, a height of 0 keeping
	// its proportions, returning the size it took. With area, it draws
	// that part of the page in the rect instead.
	Page(page int, x, y, w, h float64, area *PageArea) (float64, float64)
	// Region names a rect for the pointer events over it, reporting
	// whether the pointer is over it.
	Region(id lua.LValue, x, y, w, h float64) bool
	// Input draws the text field id of a view, w wide, Button the button
	// id, and Checkbox the check box id, each returning its size.
	Input(id string, x, y, w float64, opts InputOptions) (float64, float64)
	Button(id string, x, y, w float64, label string) (float64, float64)
	Checkbox(id string, x, y float64, label string, checked bool) (float64, float64)
	// Size is the box's size.
	Size() (w, h float64)
}

// PageArea is the part of a page a canvas draws: from the point X, Y, in
// the page's points, at the rect's top left, at Zoom times the document's
// zoom.
type PageArea struct {
	X, Y, Zoom float64
}

// InputOptions are how a text field is drawn: a placeholder shown while
// it is empty, with Secret its text hidden, as a password's, and with
// Lines more than one, that many lines, wrapping and taking newlines.
type InputOptions struct {
	Placeholder string
	Secret      bool
	Lines       int
}

// TextOptions are how a canvas draws text. Size is in logical pixels, the
// UI font's when 0. With Width, text is wrapped to it, or truncated if
// Wrap is false, and aligned within it; without, Align places text's
// left, centre or right at x.
type TextOptions struct {
	Bold, Mono, Wrap bool
	Size, Width      float64
	Align            string // left, center or right
}

// DrawState is what a draw function is told about the element it draws.
type DrawState struct {
	Element    Element
	X, Y, W, H float64 // the box, in logical pixels from the window's top left
	Alt        bool    // whether alternate-color mode is on
	Time       float64 // seconds since onon started, for drawing that moves
	Scroll     float64 // how far a view's body is scrolled, in logical pixels
}

const luaCanvasType = "onon.canvas"

// drawCache keeps what draw functions use on every frame: the canvas they
// are given, and the colours and path data they write, parsed.
type drawCache struct {
	canvas     *lua.LUserData
	box, state *lua.LTable
	depth      int    // the draw functions running
	dir        string // where image paths not absolute are from
	colors     map[string]Color
	paths      map[string][]PathOp
}

// maxDrawCacheEntries bounds each map, which is emptied when full, in
// case a function writes ever new colours or paths.
const maxDrawCacheEntries = 256

func (c *drawCache) color(s string) (Color, error) {
	if clr, ok := c.colors[s]; ok {
		return clr, nil
	}
	clr, err := parseStyleColor(s)
	if err == nil {
		if c.colors == nil || len(c.colors) >= maxDrawCacheEntries {
			c.colors = map[string]Color{}
		}
		c.colors[s] = clr
	}
	return clr, err
}

func (c *drawCache) path(s string) ([]PathOp, error) {
	if ops, ok := c.paths[s]; ok {
		return ops, nil
	}
	ops, err := ParsePath(s)
	if err == nil {
		if c.paths == nil || len(c.paths) >= maxDrawCacheEntries {
			c.paths = map[string][]PathOp{}
		}
		c.paths[s] = ops
	}
	return ops, err
}

// RunDraw runs an element's draw function as fn(canvas, box, state). The
// canvas is cleared afterwards, so one kept past the call cannot draw, and
// box and state are the same tables on every call, saving their garbage
// on every frame.
func (r *Runtime) RunDraw(fn *lua.LFunction, canvas Canvas, state DrawState) error {
	_, err := r.runDraw(fn, canvas, state, 0)
	return err
}

// runDraw runs a draw function, returning what it returned when nret is 1.
func (r *Runtime) runDraw(fn *lua.LFunction, canvas Canvas, state DrawState, nret int) (lua.LValue, error) {
	if r == nil || r.state == nil {
		return lua.LNil, fmt.Errorf("no Lua state")
	}
	L := r.state
	ud := r.draw.canvas
	if ud == nil {
		ud = L.NewUserData()
		L.SetMetatable(ud, luaCanvasMetatable(L, &r.draw))
		r.draw.canvas = ud
	}
	if r.draw.box == nil {
		r.draw.box, r.draw.state = L.NewTable(), L.NewTable()
	}
	r.draw.dir = ""
	if r.cfg.ConfigPath != "" {
		r.draw.dir = filepath.Dir(r.cfg.ConfigPath)
	}
	box, st := r.draw.box, r.draw.state
	// A draw run from another, as a row's is from a view's canvas:default(),
	// takes its own canvas and tables, leaving the outer function's as they
	// were.
	if r.draw.depth > 0 {
		ud = L.NewUserData()
		L.SetMetatable(ud, luaCanvasMetatable(L, &r.draw))
		box, st = L.NewTable(), L.NewTable()
	}
	r.draw.depth++
	ud.Value = canvas
	defer func() { ud.Value = nil; r.draw.depth-- }()
	box.RawSetString("x", lua.LNumber(state.X))
	box.RawSetString("y", lua.LNumber(state.Y))
	box.RawSetString("w", lua.LNumber(state.W))
	box.RawSetString("h", lua.LNumber(state.H))
	st.RawSetString("element", lua.LString(state.Element.String()))
	st.RawSetString("alt", lua.LBool(state.Alt))
	st.RawSetString("time", lua.LNumber(state.Time))
	st.RawSetString("scroll", lua.LNumber(state.Scroll))
	ret := lua.LValue(lua.LNil)
	err := r.doLua(func() error {
		if err := L.CallByParam(lua.P{Fn: fn, NRet: nret, Protect: true}, ud, box, st); err != nil {
			return err
		}
		if nret > 0 {
			ret = L.Get(-1)
			L.Pop(nret)
		}
		return nil
	})
	return ret, err
}

func push2(L *lua.LState, a, b float64) int {
	L.Push(lua.LNumber(a))
	L.Push(lua.LNumber(b))
	return 2
}

// imagePath is path as an image is read from: one not absolute from the
// configuration's directory.
func (c *drawCache) imagePath(path string) string {
	if !filepath.IsAbs(path) && c.dir != "" {
		return filepath.Join(c.dir, path)
	}
	return path
}

// textOptionKeys are the fields a table of text options has, which tell
// it from a colour given as a table.
var textOptionKeys = []string{"bold", "mono", "size", "width", "wrap", "align", "color"}

// textArgs reads the colour and options of text from n on: a colour then
// options, either left out, the colour also an option's field. The colour
// is the foreground unless given.
func textArgs(L *lua.LState, n int) (Color, TextOptions) {
	clr := paletteColor("foreground", 1)
	optsAt := n
	switch v := L.Get(n).(type) {
	case *lua.LNilType:
		optsAt = n + 1
	case *lua.LTable:
		isOpts := false
		for _, key := range textOptionKeys {
			isOpts = isOpts || v.RawGetString(key) != lua.LNil
		}
		if !isOpts {
			clr, optsAt = luaColorArg(L, n), n+1
		}
	case lua.LBool:
	default:
		clr, optsAt = luaColorArg(L, n), n+1
	}
	opts := luaTextOptions(L, optsAt)
	if t, ok := L.Get(optsAt).(*lua.LTable); ok {
		if c := t.RawGetString("color"); c != lua.LNil {
			parsed, err := colorFromLua(c)
			if err != nil {
				L.ArgError(optsAt, "color: "+err.Error())
			}
			clr = parsed
		}
	}
	return clr, opts
}

func luaColorArg(L *lua.LState, n int) Color {
	c, err := colorFromLua(L.Get(n))
	if err != nil {
		L.ArgError(n, err.Error())
	}
	return c
}

// luaInputOptions reads a text field's options at n.
func luaInputOptions(L *lua.LState, n int) InputOptions {
	var o InputOptions
	if opts, ok := L.Get(n).(*lua.LTable); ok {
		o.Placeholder = lua.LVAsString(opts.RawGetString("placeholder"))
		o.Secret = lua.LVAsBool(opts.RawGetString("secret"))
		o.Lines = int(lua.LVAsNumber(opts.RawGetString("lines")))
	}
	return o
}

// luaPageArea reads the part of a page to draw at n, if given, which needs
// a height to show it in.
func luaPageArea(L *lua.LState, n int, height float64) *PageArea {
	opts, ok := L.Get(n).(*lua.LTable)
	if !ok {
		return nil
	}
	area := &PageArea{X: float64(lua.LVAsNumber(opts.RawGetString("x"))), Y: float64(lua.LVAsNumber(opts.RawGetString("y"))), Zoom: 1}
	if zoom, ok := opts.RawGetString("zoom").(lua.LNumber); ok {
		if zoom <= 0 {
			L.ArgError(n, "zoom must be more than 0")
		}
		area.Zoom = float64(zoom)
	}
	if height <= 0 {
		L.ArgError(n-1, "a part of a page needs a height to show it in")
	}
	return area
}

// luaColumn lays a canvas's drawing out down a column: each thing drawn
// takes the column's width, or its own, below the last, gap apart.
type luaColumn struct {
	canvas       *lua.LUserData
	cache        *drawCache
	x, y, w, gap float64
}

const luaColumnType = "onon.canvas.column"

func luaColumnMetatable(L *lua.LState) *lua.LTable {
	mt := L.NewTypeMetatable(luaColumnType)
	if mt.RawGetString("__index") != lua.LNil {
		return mt
	}
	// at checks the column and its canvas, drawing now.
	at := func(L *lua.LState) (*luaColumn, Canvas) {
		if ud, ok := L.Get(1).(*lua.LUserData); ok {
			if col, ok := ud.Value.(*luaColumn); ok {
				if c, ok := col.canvas.Value.(Canvas); ok {
					return col, c
				}
				L.RaiseError("a column draws on its canvas while it draws, in the draw function")
			}
		}
		L.ArgError(1, "expected a column")
		return nil, nil
	}
	// next moves the column below what took h, returning its size.
	next := func(L *lua.LState, col *luaColumn, w, h float64) int {
		col.y += h + col.gap
		return push2(L, w, h)
	}
	methods := L.NewTable()
	L.SetFuncs(methods, map[string]lua.LGFunction{
		"text": func(L *lua.LState) int {
			col, c := at(L)
			clr, opts := textArgs(L, 3)
			if opts.Width == 0 {
				opts.Width = col.w
			}
			// Text wraps to the column unless told not to.
			if t, ok := L.Get(L.GetTop()).(*lua.LTable); !ok || t.RawGetString("wrap") == lua.LNil {
				opts.Wrap = true
			}
			w, h := c.Text(col.x, col.y, L.CheckString(2), clr, opts)
			return next(L, col, w, h)
		},
		"input": func(L *lua.LState) int {
			col, c := at(L)
			w, h := c.Input(L.CheckString(2), col.x, col.y, col.w, luaInputOptions(L, 3))
			return next(L, col, w, h)
		},
		"button": func(L *lua.LState) int {
			col, c := at(L)
			w, h := c.Button(L.CheckString(2), col.x, col.y, float64(L.OptNumber(4, lua.LNumber(col.w))), L.CheckString(3))
			return next(L, col, w, h)
		},
		"checkbox": func(L *lua.LState) int {
			col, c := at(L)
			w, h := c.Checkbox(L.CheckString(2), col.x, col.y, L.OptString(3, ""), L.ToBool(4))
			return next(L, col, w, h)
		},
		"page": func(L *lua.LState) int {
			col, c := at(L)
			page := L.CheckInt(2)
			if page < 1 {
				L.ArgError(2, "pages start at 1")
			}
			height := float64(L.OptNumber(3, 0))
			w, h := c.Page(page-1, col.x, col.y, col.w, height, luaPageArea(L, 4, height))
			return next(L, col, w, h)
		},
		"image": func(L *lua.LState) int {
			col, c := at(L)
			w, h, err := c.Image(col.cache.imagePath(L.CheckString(2)), col.x, col.y, float64(L.OptNumber(3, 0)), float64(L.OptNumber(4, 0)))
			if err != nil {
				L.ArgError(2, err.Error())
			}
			return next(L, col, w, h)
		},
		"space": func(L *lua.LState) int {
			col, _ := at(L)
			col.y += float64(L.CheckNumber(2))
			return 0
		},
		"y": func(L *lua.LState) int {
			col, _ := at(L)
			L.Push(lua.LNumber(col.y))
			return 1
		},
	})
	L.SetField(mt, "__index", methods)
	return mt
}

// luaTextOptions reads canvas text options at n: true for bold, or a table
// of bold, mono, size, width, wrap and align.
func luaTextOptions(L *lua.LState, n int) TextOptions {
	switch v := L.Get(n).(type) {
	case lua.LBool:
		return TextOptions{Bold: bool(v)}
	case *lua.LTable:
		o := TextOptions{
			Bold:  lua.LVAsBool(v.RawGetString("bold")),
			Mono:  lua.LVAsBool(v.RawGetString("mono")),
			Wrap:  lua.LVAsBool(v.RawGetString("wrap")),
			Size:  float64(lua.LVAsNumber(v.RawGetString("size"))),
			Width: float64(lua.LVAsNumber(v.RawGetString("width"))),
			Align: lua.LVAsString(v.RawGetString("align")),
		}
		if o.Align != "" && o.Align != "left" && o.Align != "center" && o.Align != "right" {
			L.ArgError(n, "align is left, center or right")
		}
		if o.Size < 0 || o.Width < 0 {
			L.ArgError(n, "size and width cannot be negative")
		}
		return o
	case *lua.LNilType:
		return TextOptions{}
	}
	L.ArgError(n, "expected true for bold, or a table of text options")
	return TextOptions{}
}

func luaCanvasMetatable(L *lua.LState, cache *drawCache) *lua.LTable {
	mt := L.NewTypeMetatable(luaCanvasType)
	if mt.RawGetString("__index") != lua.LNil {
		return mt
	}
	check := func(L *lua.LState) Canvas {
		if ud, ok := L.Get(1).(*lua.LUserData); ok {
			if c, ok := ud.Value.(Canvas); ok {
				return c
			}
		}
		L.ArgError(1, "expected a canvas drawing now")
		return nil
	}
	color := func(L *lua.LState, n int) Color {
		var c Color
		var err error
		if s, ok := L.Get(n).(lua.LString); ok {
			c, err = cache.color(string(s))
		} else {
			c, err = colorFromLua(L.CheckAny(n))
		}
		if err != nil {
			L.ArgError(n, err.Error())
		}
		return c
	}
	path := func(L *lua.LState, n int) []PathOp {
		if s, ok := L.Get(n).(lua.LString); ok {
			ops, err := cache.path(string(s))
			if err != nil {
				L.ArgError(n, err.Error())
			}
			return ops
		}
		return checkLuaPath(L, n).ops
	}
	methods := L.NewTable()
	L.SetFuncs(methods, map[string]lua.LGFunction{
		"default": func(L *lua.LState) int {
			check(L).Default()
			return 0
		},
		"fill": func(L *lua.LState) int {
			check(L).Fill(path(L, 2), color(L, 3))
			return 0
		},
		"stroke": func(L *lua.LState) int {
			check(L).Stroke(path(L, 2), color(L, 3), float64(L.OptNumber(4, 1)))
			return 0
		},
		"rect": func(L *lua.LState) int {
			c := check(L)
			x, y, w, h := float64(L.CheckNumber(2)), float64(L.CheckNumber(3)), float64(L.CheckNumber(4)), float64(L.CheckNumber(5))
			radius := min(max(0, float64(L.OptNumber(7, 0))), w/2, h/2)
			c.Fill(RoundedRectPath(x, y, w, h, corners(radius)), color(L, 6))
			return 0
		},
		"text": func(L *lua.LState) int {
			c := check(L)
			clr, opts := textArgs(L, 5)
			w, h := c.Text(float64(L.CheckNumber(2)), float64(L.CheckNumber(3)), L.CheckString(4), clr, opts)
			return push2(L, w, h)
		},
		"measure": func(L *lua.LState) int {
			w, h := check(L).Measure(L.CheckString(2), luaTextOptions(L, 3))
			L.Push(lua.LNumber(w))
			L.Push(lua.LNumber(h))
			return 2
		},
		"clip": func(L *lua.LState) int {
			c := check(L)
			if L.GetTop() < 2 {
				c.Unclip()
				return 0
			}
			c.Clip(float64(L.CheckNumber(2)), float64(L.CheckNumber(3)), float64(L.CheckNumber(4)), float64(L.CheckNumber(5)))
			return 0
		},
		"image": func(L *lua.LState) int {
			c := check(L)
			w, h, err := c.Image(cache.imagePath(L.CheckString(2)), float64(L.CheckNumber(3)), float64(L.CheckNumber(4)), float64(L.OptNumber(5, 0)), float64(L.OptNumber(6, 0)))
			if err != nil {
				L.ArgError(2, err.Error())
			}
			return push2(L, w, h)
		},
		"page": func(L *lua.LState) int {
			c := check(L)
			page := L.CheckInt(2)
			if page < 1 {
				L.ArgError(2, "pages start at 1")
			}
			area := luaPageArea(L, 7, float64(L.OptNumber(6, 0)))
			w, h := c.Page(page-1, float64(L.CheckNumber(3)), float64(L.CheckNumber(4)), float64(L.CheckNumber(5)), float64(L.OptNumber(6, 0)), area)
			return push2(L, w, h)
		},
		"input": func(L *lua.LState) int {
			c := check(L)
			w, h := c.Input(L.CheckString(2), float64(L.CheckNumber(3)), float64(L.CheckNumber(4)), float64(L.CheckNumber(5)), luaInputOptions(L, 6))
			return push2(L, w, h)
		},
		"button": func(L *lua.LState) int {
			c := check(L)
			w, h := c.Button(L.CheckString(2), float64(L.CheckNumber(3)), float64(L.CheckNumber(4)), float64(L.CheckNumber(5)), L.CheckString(6))
			return push2(L, w, h)
		},
		"checkbox": func(L *lua.LState) int {
			c := check(L)
			w, h := c.Checkbox(L.CheckString(2), float64(L.CheckNumber(3)), float64(L.CheckNumber(4)), L.OptString(5, ""), L.ToBool(6))
			return push2(L, w, h)
		},
		"column": func(L *lua.LState) int {
			c := check(L)
			col := &luaColumn{canvas: L.Get(1).(*lua.LUserData), gap: 8, cache: cache}
			w, _ := c.Size()
			if opts, ok := L.Get(2).(*lua.LTable); ok {
				col.x = float64(lua.LVAsNumber(opts.RawGetString("x")))
				col.y = float64(lua.LVAsNumber(opts.RawGetString("y")))
				if gap, ok := opts.RawGetString("gap").(lua.LNumber); ok {
					col.gap = float64(gap)
				}
				col.w = float64(lua.LVAsNumber(opts.RawGetString("width")))
			}
			if col.w <= 0 {
				col.w = max(0, w-2*col.x) // as far in from the right as from the left
			}
			ud := L.NewUserData()
			ud.Value = col
			L.SetMetatable(ud, luaColumnMetatable(L))
			L.Push(ud)
			return 1
		},
		"region": func(L *lua.LState) int {
			c := check(L)
			id := L.CheckAny(2)
			L.Push(lua.LBool(c.Region(id, float64(L.CheckNumber(3)), float64(L.CheckNumber(4)), float64(L.CheckNumber(5)), float64(L.CheckNumber(6)))))
			return 1
		},
		"path": func(L *lua.LState) int {
			ud, _ := newLuaPath(L)
			L.Push(ud)
			return 1
		},
	})
	L.SetField(mt, "__index", methods)
	return mt
}
