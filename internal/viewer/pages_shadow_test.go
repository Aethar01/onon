package viewer

import (
	"testing"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// A page's shadow falls beside the other pages on screen, not over them:
// what is left of where it reaches once they are taken out covers none of
// them, loses nothing else, and is the whole of it with no page near.
func TestRectsOutsideLeavesOtherPagesOut(t *testing.T) {
	own := sdl.Rect{X: 100, Y: 100, W: 200, H: 300}
	below := sdl.Rect{X: 100, Y: 400, W: 200, H: 300} // no gap between them
	beside := sdl.Rect{X: 300, Y: 100, W: 200, H: 300}
	far := sdl.Rect{X: 100, Y: 2000, W: 200, H: 300}
	reach := sdl.Rect{X: 80, Y: 80, W: 240, H: 340}

	if parts, whole := rectsOutside(reach, []sdl.Rect{own, far}, own); !whole || len(parts) != 1 || parts[0] != reach {
		t.Fatalf("with no page near: %v, whole %v; want all of %v", parts, whole, reach)
	}
	parts, whole := rectsOutside(reach, []sdl.Rect{own, below, beside, far}, own)
	if whole {
		t.Fatal("pages reach into the shadow, but it is drawn whole")
	}
	area := int32(0)
	for i, p := range parts {
		area += p.W * p.H
		for _, page := range []sdl.Rect{below, beside} {
			if cut := intersectRects(p, page); cut.W > 0 && cut.H > 0 {
				t.Fatalf("part %v of the shadow falls on the page at %v", p, page)
			}
		}
		for _, q := range parts[:i] {
			if cut := intersectRects(p, q); cut.W > 0 && cut.H > 0 {
				t.Fatalf("parts %v and %v overlap, so the shadow is drawn twice there", p, q)
			}
		}
	}
	covered := intersectRects(reach, below)
	covered2 := intersectRects(reach, beside)
	if want := reach.W*reach.H - covered.W*covered.H - covered2.W*covered2.H; area != want {
		t.Fatalf("parts cover %d pixels, want %d: all the shadow reaches but the pages", area, want)
	}
}
