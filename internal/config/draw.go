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
	// its proportions, returning the size it took.
	Page(page int, x, y, w, h float64) (float64, float64)
	// Region names a rect for the pointer events over it, reporting
	// whether the pointer is over it.
	Region(id lua.LValue, x, y, w, h float64) bool
	// Input draws the text field id of a view, w wide, returning its
	// height.
	Input(id string, x, y, w float64, opts InputOptions) float64
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
			w, h := c.Text(float64(L.CheckNumber(2)), float64(L.CheckNumber(3)), L.CheckString(4), color(L, 5), luaTextOptions(L, 6))
			L.Push(lua.LNumber(w))
			L.Push(lua.LNumber(h))
			return 2
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
			path := L.CheckString(2)
			if !filepath.IsAbs(path) && cache.dir != "" {
				path = filepath.Join(cache.dir, path)
			}
			w, h, err := c.Image(path, float64(L.CheckNumber(3)), float64(L.CheckNumber(4)), float64(L.OptNumber(5, 0)), float64(L.OptNumber(6, 0)))
			if err != nil {
				L.ArgError(2, err.Error())
			}
			L.Push(lua.LNumber(w))
			L.Push(lua.LNumber(h))
			return 2
		},
		"page": func(L *lua.LState) int {
			c := check(L)
			page := L.CheckInt(2)
			if page < 1 {
				L.ArgError(2, "pages start at 1")
			}
			w, h := c.Page(page-1, float64(L.CheckNumber(3)), float64(L.CheckNumber(4)), float64(L.CheckNumber(5)), float64(L.OptNumber(6, 0)))
			L.Push(lua.LNumber(w))
			L.Push(lua.LNumber(h))
			return 2
		},
		"input": func(L *lua.LState) int {
			c := check(L)
			var o InputOptions
			if opts, ok := L.Get(6).(*lua.LTable); ok {
				o.Placeholder = lua.LVAsString(opts.RawGetString("placeholder"))
				o.Secret = lua.LVAsBool(opts.RawGetString("secret"))
				o.Lines = int(lua.LVAsNumber(opts.RawGetString("lines")))
			}
			L.Push(lua.LNumber(c.Input(L.CheckString(2), float64(L.CheckNumber(3)), float64(L.CheckNumber(4)), float64(L.CheckNumber(5)), o)))
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
