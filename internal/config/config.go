package config

import (
	"maps"
	"slices"

	lua "github.com/yuin/gopher-lua"
)

type Config struct {
	ConfigPath            string
	StatusBarVisible      bool
	RenderMode            string
	Renderer              string
	RenderOversample      float64
	MinZoom               float64
	MaxZoom               float64
	PinchSensitivity      float64
	ZoomStep              float64
	PageCacheMemoryMB     int
	LinkSchemes           []string
	HTTPHeaders           map[string]map[string]string // by host, for remote documents
	Menus                 map[string]MenuPlacement     // by MenuNames, where onon's own menus sit
	RenderThreads         int
	MuPDFStoreMB          int
	HintChars             string
	AltColorsKeepImages   bool
	TrimMargins           bool
	AnnotationColors      []string
	LinkPreview           bool
	DualPage              bool
	FirstPageOffset       bool
	FitMode               string
	AnchorPosition        string
	Theme                 Theme
	OverviewThumbWidth    int
	OverviewMaxColumns    int
	OverviewGap           int
	LoadingIndicator      bool
	AltColors             bool
	AltColorsSystem       bool // follow the OS's dark mode instead of AltColors
	PageGap               int
	SpreadGap             int
	PageGapVertical       int
	PageGapHorizontal     int
	ScrollStep            int
	ScrollOff             int
	StatusBarLeft         string
	StatusBarRight        string
	SequenceTimeoutMS     int
	AnimationFrameMS      int
	NormalMessage         string
	KeyBindings           map[string]string
	MouseBindings         map[string]string
	MouseTextSelect       bool
	CopyOnSelect          bool
	SmoothScrollSources   SmoothInputSources
	SmoothZoomSources     SmoothInputSources
	InvertScrollSources   SmoothInputSources
	SmoothScrollDampening float64
	SmoothZoomDampening   float64
	AutoscrollSpeedFactor float64
	AutoscrollMaxSpeed    float64
	SessionDatabase       bool
	OpenInNewWindow       bool
	AntiAliasing          int
	OutlineInitialDepth   int
	OutlineWidthPercent   int
	OutlineHeightPercent  int
	CompletionMaxItems    int
	RecentFilesMax        int
	AutoReload            bool
	AutoReloadDelayMS     int
	LinkPreviewDelayMS    int
	PromptHistoryMax      int
	TrimPadding           int
}

type Runtime struct {
	luaGeneration
	explicitPath     string
	docPath          string
	docName          string
	docMeta          documentMeta
	host             Host
	uiSeq            int
	luaCallDepth     int
	deferredOpen     string
	assigned         map[string]bool // built-in options assigned since ConsumeDirty
	verbose          bool
	pluginPaths      []string
	disabledPlugins  []string
	noConfig         bool
	operationResults chan pluginOperationResult
	wake             func() // wakes the viewer to poll a delivered result; may be nil
	nextOperationID  int
	loadingPlugin    string
	activePlugin     string
	warnings         []string // see warn
}

// luaGeneration is what one load of the configuration builds. Reload
// replaces it as a whole and, if loading fails, puts the old one back.
type luaGeneration struct {
	state       *lua.LState
	cfg         Config
	callbacks   map[string]*lua.LFunction
	callbackSeq int
	// openViews has, for each plugin view shown, what takes its state as
	// it closes; see UIViewClosed.
	openViews        map[string]func(UIViewState)
	schemes          map[string]*lua.LFunction // onon.register_scheme, by name
	dirty            bool
	pluginCatalog    *pluginCatalog
	plugins          *pluginState
	operations       map[int]*pluginOperation
	pluginGeneration int
	draw             drawCache
	// loadingConfig is set while config.lua runs, which records its
	// changes to the theme in themeOps and whether it assigns the theme
	// whole in configSetsTheme; themeChoice is the theme picked with
	// :theme, if any. See theme_choice.go.
	loadingConfig   bool
	themeOps        []themeOp
	configSetsTheme bool
	themeChoice     string
}

type UIOverlay struct {
	ID         string
	Title      string
	Rows       []UIListRow
	Selected   int
	Scroll     int
	Query      string
	Searchable bool
	// Filter is whether the query filters the rows; a view that sets its
	// own rows for each query in OnQueryChanged turns it off.
	Filter bool
	// Modal views take the keys and close on a click outside them, as
	// menus do. Other views are panels: they stay open over the document,
	// and under any menu, taking only the pointer over them.
	Modal bool
	// Width and Height are the view's size. Anchor is where it sits in
	// the document's part of the window: center, or an edge or corner such
	// as top or bottom_right. With At, it is instead the part of the view
	// that sits at that point. A docked view takes a side of the window
	// from the document.
	Width, Height UISize
	Anchor        string
	At            *UIPoint
	Dock          string
	// Resizable lets the pointer drag a docked view's inner edge.
	Resizable bool
	// Stack opens a modal view over the menu open, which it returns to,
	// instead of replacing it.
	Stack bool
	// Prompt, if set, puts a text field in the header, labelled with it,
	// whose text is the query. Fields are the text fields' first texts,
	// by id.
	Prompt string
	Fields map[string]string
	// Empty is what an empty list says.
	Empty string
	// Columns, if set, lay rows' cells out as a table.
	Columns []UIColumn
	// The callbacks, each the ID of a registered Lua function or empty.
	OnSelect, OnClose, OnSelectionChanged, OnQueryChanged string
	OnKey, OnAction, OnMouse, Draw, OnInput, OnSubmit     string
	OnClick                                               string
	Generation                                            int
}

// MenuViews are onon's own menus that the menus option places, by name,
// each with the id of its view, and MenuNames their names, sorted.
var (
	MenuViews = map[string]string{
		"annotation": "annotation-menu", "choice": "form-choice", "colors": "color-picker", "help": "help",
		"keybind_keys": "core:keybind-keys", "keybinds": "core:keybinds", "matches": "matches", "options": "options",
		"outline": "core:outline", "print": "print", "printers": "printers", "recent": "recent-files", "themes": "themes",
	}
	MenuNames = slices.Sorted(maps.Keys(MenuViews))
)

// MenuPlacement is where one of onon's own menus sits, each field left
// zero keeping the menu's own: the most of the window it takes, as a menu
// narrows to its rows, and the part of the window it sits in.
type MenuPlacement struct {
	Width, Height UISize
	Anchor        string
}

// UIAnchors are the places a view may be anchored, and UIDocks the sides
// it may be docked to.
var (
	UIAnchors = []string{"center", "top", "bottom", "left", "right", "top_left", "top_right", "bottom_left", "bottom_right"}
	UIDocks   = []string{"left", "right", "top", "bottom"}
)

// UIColumn is a column of a table of rows: its title, its width, or with
// Fill a share of what the others leave, and how its cells align.
type UIColumn struct {
	Title string
	Width UISize
	Fill  bool
	Align string // left, center or right
}

// UISize is a length in logical pixels or, with Percent, a percentage of
// the window's.
type UISize struct {
	Value   float64
	Percent bool
}

// UIPoint is a point in the window in logical pixels or, with Page set,
// a point on that page (1-based) in the page's points, which moves with
// the page.
type UIPoint struct {
	Page int
	X, Y float64
}

type UIListRow struct {
	Text      string
	Value     string
	ID        string
	Secondary string
	Depth     int
	Disabled  bool
	Key       string // a key binding, drawn in a column before the text
	Marker    string // drawn just before the text
	Heading   bool   // a section title, which cannot be chosen
	Swatch    *Color // a colour sample drawn before the text
	Button    bool   // drawn as a button
	// Checkbox draws a check box before the text, checked with Checked.
	Checkbox, Checked bool
	// Cells are the row's text in its view's columns.
	Cells []string
	// Lua is the row as the plugin gave it, handed back to callbacks.
	Lua lua.LValue
}

// UIViewState is a view's state as it closed: its fields' text, its
// selection (1-based), scroll and query, and its size in logical pixels.
type UIViewState struct {
	Fields           map[string]string
	Selected, Scroll int
	Query            string
	W, H             float64
}

// UIRowRef is a row as a view's callbacks are given it.
type UIRowRef struct {
	Index           int // 1-based, in the rows as set, however filtered
	Value, Text, ID string
	Lua             lua.LValue
}

// UIMouseEvent is a pointer event over a view. X and Y are logical
// pixels from the top left of the view's body, as its canvas draws.
type UIMouseEvent struct {
	Type   string // press, release, move, leave or wheel
	Button string // left, middle, right, x1 or x2, for press and release
	X, Y   float64
	Clicks int     // 2 for a double click, for press
	DX, DY float64 // for wheel: positive is right and down
	// Region is the id of the region the canvas last drew under the
	// pointer, the one drawn last where they overlap.
	Region lua.LValue
}

type documentMeta struct {
	exists    bool
	sizeBytes int64
	ext       string
	pageCount int
	hasPages  bool
}

type Host interface {
	ExecuteAction(action string) error
	Open(path string) error
	ShowUI(overlay UIOverlay) error
	CloseUI(id string)
	UIVisible(id string) bool
	SetUIRows(id string, rows []UIListRow)
	SetUISelected(id string, selected int)
	SetUIScroll(id string, scroll int)
	SetUIQuery(id string, query string)
	UISelected(id string) int
	UIScroll(id string) int
	UIQuery(id string) string
	// RedrawUI draws the view again, as a view with a draw function needs
	// when what it draws changes.
	RedrawUI(id string)
	// SetUITitle changes a view's title, and UIFields gives the text of
	// all its text fields.
	SetUITitle(id, title string)
	UIFields(id string) map[string]string
	// UIField and SetUIField get and set the text of a view's text field.
	UIField(id, field string) string
	SetUIField(id, field, text string)
	// FocusUI gives a view the keys, or with focus false takes them back.
	FocusUI(id string, focus bool)
	UIFocused(id string) bool
	// UIViewSize is a view's size in logical pixels.
	UIViewSize(id string) (w, h float64)
	Page() int
	PageCount() int
	GotoPage(page int) error
	GotoDocumentPoint(page int, x, y float64) error
	Message() string
	SetMessage(message string)
	RunCommand(command string) error
	Mode() string
	Search(query string, backward bool) error
	SearchQuery() string
	SearchMatchCount() int
	SearchMatchIndex() int
	CurrentCount() string
	PendingKeys() []string
	ClearPendingKeys()
	FitMode() string
	SetFitMode(mode string) error
	RenderMode() string
	SetRenderMode(mode string) error
	Zoom() float64
	SetZoom(zoom float64) error
	Rotation() float64
	SetRotation(rotation float64) error
	Fullscreen() bool
	SetFullscreen(fullscreen bool) error
	CacheEntries() int
	CachePending() int
	CacheLimit() int
	SetCacheLimit(limit int) error
	ClearCache()
}

type ClipboardGetter interface {
	GetClipboard() string
}

type ClipboardSetter interface {
	SetClipboard(text string) error
}

type ExternalOpener interface {
	OpenExternal(uri string) error
}

type DirectoryPicker interface {
	PickDirectory() (string, error)
}

// DocumentFormatHost reports which formats the document engine can open. It is
// optional so hosts without a document engine still satisfy Host.
type DocumentFormatHost interface {
	SupportedExtensions() []string
	SupportsPath(path string) bool
}
