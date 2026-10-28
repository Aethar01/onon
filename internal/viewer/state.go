package viewer

import (
	"image/color"
	"sync"
	"time"

	"onon/internal/config"
	"onon/internal/instance"
	"onon/internal/mupdf"
	"onon/internal/remote"

	"github.com/jupiterrider/purego-sdl3/sdl"
	"golang.org/x/image/font"
)

type mode int

const (
	modeNormal mode = iota
	modeCommand
	modeGotoPage
	modeSearch
	modePassword
	modePrompt // a one-off question answered through a.promptState
)

// pageMetrics holds a page's geometry. bounds is what layout and rendering
// use: the full page, or its content box while margins are trimmed.
type pageMetrics struct {
	bounds     mupdf.Rect
	full       mupdf.Rect
	content    mupdf.Rect
	hasContent bool
	width      float64
	height     float64
	label      string
	loaded     bool
}

// textSelection runs from anchor on anchorPage to focus on focusPage,
// either of which may come first.
type textSelection struct {
	active     bool
	mode       mupdf.SelectMode // words or lines after a double or triple click
	anchorPage int
	anchor     mupdf.Point
	focusPage  int
	focus      mupdf.Point
	parts      []selectionPart // highlighted quads, one entry per page in order
	text       string
	wholePages map[int]*mupdf.Selection // pages between the ends, see selectionOnPage
	// stale is set while the focus has moved since parts and text were
	// extracted; see refreshStaleSelection.
	stale bool
}

type selectionPart struct {
	page  int
	quads []mupdf.Quad
}

func (s textSelection) empty() bool { return s.text == "" && len(s.parts) == 0 }

type viewportAnchor struct {
	page  int
	point mupdf.Point
	valid bool
}

// rowLayout is a row of pages. In the base rows that layout starts from,
// width is the pages' total unscaled width and gaps the screen pixels
// between them; once laid out, width is the row's full screen width.
type rowLayout struct {
	pages  []int
	x      float64
	y      float64
	width  float64
	gaps   float64
	height float64
	// Before each page in the row come gapBefore screen pixels and
	// padBefore unscaled units of blank space.
	gapBefore []float64
	padBefore []float64
	pageX     []float64
	pageY     []float64
	pageW     []float64
	pageH     []float64
}

type documentWorkers struct {
	renderWorker *renderWorker
	metricLoader *metricLoader
	searchWorker *searchWorker
}

type documentState struct {
	documentAPIMu sync.Mutex
	viewEvents    viewStateEvents
	// instanceServer is nil unless single-instance handling is enabled.
	singleInstance         bool
	instanceServer         *instance.Server
	instanceAddress        string
	pendingInstanceJump    *instanceJump
	pendingInstanceCommand string
	docPath                string // what the document is known by: a path, or a remote URI
	docFile                string // the file read: docPath, or a remote document's cached copy
	releaseCopy            func() // lets the cache remove a remote document's copy, once read no more
	docName                string
	docPassword            string
	doc                    *mupdf.Document

	pageCount  int
	page       int
	generation int
	pageLinks  map[int][]mupdf.Link
	outline    []mupdf.OutlineItem

	initialDocPath   string
	initialStartPage int
	initialPageSet   bool
	document         documentSession

	// remoteInfo is the version of a remote document that was opened, and
	// remoteTask a fetch or save in progress.
	remoteInfo remote.Info
	remoteTask *remoteTask
	// remotePrompts brings questions from ssh and web servers to the event
	// loop; remotePrompt is the one being asked.
	remotePrompts chan *remotePrompt
	remotePrompt  *remotePrompt
}

type viewStateFields struct {
	rotation        float64
	zoom            float64
	fitMode         fitMode
	renderMode      renderMode
	scale           float64
	dualPage        bool
	firstPageOffset bool
	statusBarShown  bool
	fullscreen      bool
	scrollX         float64
	scrollY         float64
	pageStep        float64
	altColors       bool
	trimMargins     bool
}

type layoutState struct {
	rows      []rowLayout
	pageToRow []int
	contentW  float64
	contentH  float64
	winW      int
	winH      int
}

type sdlState struct {
	waker       *loopWaker
	window      *sdl.Window
	renderer    *sdl.Renderer
	cursors     map[cursorKind]*sdl.Cursor
	cursor      cursorKind // the cursor currently shown
	iconBytes   []byte
	fontFace    font.Face
	headingFace font.Face  // heavier, for titles; may be fontFace
	uiScale     float64    // output pixels per logical pixel the UI font was loaded at
	fontWarning string     // the last font warning logged, so a reload does not repeat it
	clips       []sdl.Rect // the clip rects withClip has in place, innermost last
	textCache   textTextureCache
	masks       maskCache                          // the theme's shapes, rasterised
	styles      *[config.ElementCount]config.Style // the theme's styles, resolved; nil until drawn
	pathKeys    pathKeys                           // draw functions' paths, packed
	canvas      canvasCache                        // what canvases draw beyond the UI font
	drawRects   [2]sdl.FRect                       // rects handed to SDL while drawing, kept to save allocating them
	frameStart  time.Time                          // when the current animation frame began
	// displayFrame is the window's display's refresh interval, or 0 when
	// unknown; see animationFrameDuration.
	displayFrame time.Duration
	// windowColors are the background and border last given to the
	// window's frame.
	windowColors [2]color.RGBA
	// menuBarShown is set once the menu bar shows the key bindings.
	menuBarShown bool
	// drawing is set while drawFrame runs; frameStatus is the status bar's
	// layout for that frame, once something has asked for it.
	drawing     bool
	frameStatus *statusLayout
	// themeErrors are the theme's shapes and draw functions that failed,
	// each reported once.
	themeErrors map[any]bool
	// autoscrollMarker is the drawn autoscroll anchor, for the size and
	// axes in autoscrollMarkerKey.
	autoscrollMarker    *sdl.Texture
	autoscrollMarkerKey autoscrollMarkerKey
	// titleBar is the window's own title bar, where the system draws none.
	titleBar *titleBar
}

// linkInputState tracks the link under the pointer.
type linkInputState struct {
	pressed *mupdf.Link
	// hoverMessage is the target shown while hovering a link, and
	// messageBeforeHover the status message it replaced.
	hoverMessage       string
	messageBeforeHover string
	overLink           bool
}

type inputState struct {
	mode           mode
	input          textInput
	ignoreText     string
	message        string
	links          linkInputState
	touch          touchState
	hints          *hintState     // non-nil while link hints are shown
	overview       *overviewState // non-nil while the page overview is shown
	presentation   *presentationState
	histories      map[string]*promptHistory
	unsaved        bool        // the document has edits not yet written
	highlightColor int         // palette index last used for highlights
	promptState    promptState // the question asked in modePrompt
	printSettings  printSettings
	printResult    chan string // delivers the outcome of a print in progress
	printers       printerList
	printersFound  chan printerList // delivers a printer lookup in progress
	preview        *linkPreview
	discardWarned  bool // the user was told that unsaved edits would be lost
	editPos        int  // see markEdited
	editEnd        int
	savedPos       int
	pointer        sdl.FPoint // last pointer position, for actions on what is under it
	inputScroll    int        // pixels the status-bar prompt is scrolled left
	// composition is what the input method is composing, shown at the
	// cursor of the text typed into until typed; textInputArea is where it
	// was last told the cursor is.
	composition    string
	textInputArea  sdl.Rect
	passwordPrompt pendingPasswordPrompt
	mouseBindings  map[string]string
	searchInput    searchMode
	sequence       []string
	sequenceAt     time.Time
	sequenceLookup map[string]string
	// sequencePrefixes holds every proper prefix of a bound sequence.
	sequencePrefixes map[string]struct{}
	pendingCount     string
	pendingMark      string
	inputSource      smoothInputSource
	// lastTrackpadWheel is the SDL timestamp of the last wheel event read
	// as a trackpad's.
	lastTrackpadWheel uint64
	smoothZoom        *smoothZoomState
	smoothScroll      *smoothScrollState
}

type pendingPasswordPrompt struct {
	path string
	opts openDocumentOptions
}

type interactionState struct {
	selection  textSelection
	panning    bool
	panButton  uint8
	panKeycode sdl.Keycode
	autoscroll *autoscrollState
	// swallowButtonUp is a mouse button whose press ended autoscroll, so
	// its release is ignored too.
	swallowButtonUp uint8
	mouseButton     uint8
	// actionKeycode is the key whose press is running the current action,
	// or 0.
	actionKeycode sdl.Keycode
	lastKeyUpCode sdl.Keycode
	lastKeyUpAt   time.Time
}

type uiState struct {
	pendingRedraw bool
	search        searchState
	views         uiManager
	outlineMenu   outlineMenuState
	keybindMenu   keybindMenuState
	completion    completionState
	motion        motionState
}

type navigationState struct {
	quit        bool
	pendingOpen string
	jumpBack    []jumpPosition
	jumpAhead   []jumpPosition
}

type jumpPosition struct {
	page    int
	scrollX float64
	scrollY float64
}
