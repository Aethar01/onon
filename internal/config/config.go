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
	Prompts               map[string]PromptPlacement   // by PromptNames, where prompts sit
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
	OpenDocuments         string // tab, window or replace
	AntiAliasing          int
	OutlineInitialDepth   int
	OutlineWidthPercent   int
	OutlineHeightPercent  int
	CompletionMaxItems    int
	RecentFilesMax        int
	RestoreTabs           bool
	NativeTabs            bool
	TabBarPosition        string // title, top, bottom, left or right
	TabBarVisible         string // auto, always or never
	TabBarResizable       bool
	TabBarOrder           int
	TabBarDragSpace       float64 // kept clear for dragging the window, in the title row
	TabWidth              TabWidth
	TabDrag               bool
	StatusBarOrder        int
	AutoReload            bool
	AutoReloadDelayMS     int
	LinkPreviewDelayMS    int
	PromptHistoryMax      int
	TrimPadding           int
}

type Runtime struct {
	luaGeneration
	explicitPath string
	docPath      string
	docName      string
	docMeta      documentMeta
	host         Host
	uiSeq        int
	luaCallDepth int
	// deferred are viewer calls Lua made, such as onon.open, run once the
	// Lua that made them returns, as they may replace the Lua state.
	deferred         []func() error
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
	openViews     map[string]func(UIViewState)
	schemes       map[string]*lua.LFunction // onon.register_scheme, by name
	dirty         bool
	pluginCatalog *pluginCatalog
	plugins       *pluginState
	operations    map[int]*pluginOperation
	// backgroundDocuments are the documents this Lua state opened without
	// a tab, by id, closed with it.
	backgroundDocuments map[int]bool
	pluginGeneration    int
	draw                drawCache
	// loadingConfig is set while config.lua runs, which records its
	// changes to the theme in themeOps and whether it assigns the theme
	// whole in configSetsTheme; themeChoice is the theme picked with
	// :theme, if any. See theme_choice.go.
	loadingConfig   bool
	themeOps        []themeOp
	configSetsTheme bool
	themeChoice     string
}

// UIAside is a part of a list view beside its rows, on its Side, left or
// right, under its header, Width wide: a share of the view's width, or
// logical pixels. With Grow the view widens by it; without, the rows give
// way. The Lua function Draw draws in it.
type UIAside struct {
	Side  string
	Width UISize
	Grow  bool
	// MinRows is the fewest rows tall the view is while it has the aside,
	// however few rows it has.
	MinRows int
	Draw    string
}

type UIOverlay struct {
	ID    string
	Title string
	// Detail is drawn muted on the right of the header, as onon's own
	// menus draw their counts.
	Detail string
	// Aside, if set, is a part of a list view beside its rows.
	Aside      *UIAside
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
	// Attach, if set, attaches the view to another's selected row or to
	// a box, Anchor being where it sits while that is not shown.
	Attach *UIAttach
	Dock   string
	// Order places a docked view among the bars and views along the
	// window's edges: lower is nearer the edge. See DockOrderPanel.
	Order int
	// Resizable lets the pointer drag a docked view's inner edge.
	Resizable bool
	// Motion, if set, is the view's own motion over the theme's.
	Motion *ViewMotion
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
		"outline": "core:outline", "print": "print", "printers": "printers", "recent": "recent-files", "tabs": "tabs", "themes": "themes",
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

// MenuBeside is the menus option's anchor for a menu opened over another,
// such as keybind_keys over keybinds: beside the row it was opened from.
const MenuBeside = "beside"

// PromptNames are the prompts the prompts option places: the command line,
// go to page, a document's password, a plugin's question, and search.
var PromptNames = []string{"command", "go_to", "password", "prompt", "search"}

// PromptPlacement is where a prompt sits: with no anchor in the status
// bar, as by default, or else in a box of its own at the anchor, Width
// wide or, left zero, half the window.
type PromptPlacement struct {
	Width  UISize
	Anchor string
}

// UIAnchors are the places a view may be anchored, and UIDocks the sides
// it may be docked to.
var (
	UIAnchors = []string{"center", "top", "bottom", "left", "right", "top_left", "top_right", "bottom_left", "bottom_right"}
	UIDocks   = []string{"left", "right", "top", "bottom"}
)

// The order of what lines the window's edges, unless set otherwise: each
// takes its side of what those of lower order leave, so the status bar
// spans the window below a tab bar at the left, and a panel docked at the
// bottom sits above both.
const (
	DockOrderStatusBar = 10
	DockOrderTabBar    = 20
	DockOrderPanel     = 30
)

// TabBarPositions are where the tab bar may sit: title is along the top,
// in the row of the window's own buttons.
var TabBarPositions = []string{"title", "top", "bottom", "left", "right"}

// TabWidth is how wide tabs are: Mode fit, as wide as their names; fill,
// sharing the bar; or empty, Size wide. A bar at the side is as wide as
// its tabs.
type TabWidth struct {
	Mode string
	Size UISize
}

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

// UIAttach is what a view is attached to: the selected row of the view
// whose id is View, as a submenu is, or else Box, as a dropdown is; and
// Side, the side of it the view sits on first, right of a row and below a
// box unless given.
type UIAttach struct {
	View string
	Box  UIBox
	Side string
}

// UIAttachSides are the sides a view may be attached on.
var UIAttachSides = []string{"right", "left", "below", "above"}

// UIBox is a box in the window, in logical pixels, or with Page on a page,
// in its points from its top left.
type UIBox struct {
	Page       int
	X, Y, W, H float64
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
	Hint      string // shown on the right while the row is selected and has no secondary text
	// Checkbox draws a check box before the text, checked with Checked.
	Checkbox, Checked bool
	// Cells are the row's text in its view's columns.
	Cells []string
	// Edit, if set, has choosing the row type into it in place of its
	// secondary text, rather than run on_select.
	Edit *UIRowEdit
	// Lua is the row as the plugin gave it, handed back to callbacks.
	Lua lua.LValue
}

// UIRowEdit is how a row is typed into: the text it starts from, a hint
// at what to type, shown beside it while there is room, and what shows
// while nothing is typed.
type UIRowEdit struct {
	Text, Hint, Placeholder string
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
	// SetUITitle and SetUIDetail change a view's title and the detail on
	// the right of its header, and UIFields gives the text of all its text
	// fields.
	SetUITitle(id, title string)
	SetUIDetail(id, detail string)
	// SetUIAside gives a view an aside, or takes it away for nil.
	SetUIAside(id string, aside *UIAside)
	UIFields(id string) map[string]string
	// UIField and SetUIField get and set the text of a view's text field.
	UIField(id, field string) string
	SetUIField(id, field, text string)
	// EditUIRow starts typing into a view's row, index 1-based.
	EditUIRow(id string, index int)
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
