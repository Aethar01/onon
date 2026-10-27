package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"onon/internal/actions"
	"onon/internal/keys"
)

type stubHost struct {
	actions          []string
	opened           string
	ui               UIOverlay
	uiVisible        bool
	uiClosed         bool
	page             int
	pages            int
	message          string
	commands         []string
	mode             string
	searchQuery      string
	searchBackward   bool
	searchMatchCount int
	searchMatchIndex int
	currentCount     string
	pendingKeys      []string
	fitMode          string
	renderMode       string
	zoom             float64
	rotation         float64
	fullscreen       bool
	cacheEntries     int
	cachePending     int
	cacheLimit       int
	cacheCleared     bool
}

type reloadingOpenHost struct {
	stubHost
	rt *Runtime
}

func uiListRowTexts(rows []UIListRow) []string {
	texts := make([]string, len(rows))
	for i, row := range rows {
		texts[i] = row.Text
	}
	return texts
}

func (h *reloadingOpenHost) Open(path string) error {
	h.opened = path
	return h.rt.SetDocument(path)
}

func (h *stubHost) ExecuteAction(action string) error {
	h.actions = append(h.actions, action)
	return nil
}

func (h *stubHost) Open(path string) error {
	h.opened = path
	return nil
}

func (h *stubHost) ShowUI(overlay UIOverlay) error {
	h.ui = overlay
	h.uiVisible = true
	return nil
}

func (h *stubHost) CloseUI(id string) {
	h.uiVisible = false
	h.uiClosed = true
}

func (h *stubHost) UIVisible(id string) bool { return h.uiVisible && h.ui.ID == id }

func (h *stubHost) SetUIRows(id string, rows []UIListRow) {
	h.ui.Rows = append([]UIListRow(nil), rows...)
}

func (h *stubHost) SetUISelected(id string, selected int) { h.ui.Selected = selected }

func (h *stubHost) SetUIScroll(id string, scroll int) { h.ui.Scroll = scroll }

func (h *stubHost) SetUIQuery(id string, query string) { h.ui.Query = query }

func (h *stubHost) UISelected(id string) int { return h.ui.Selected }

func (h *stubHost) UIScroll(id string) int { return h.ui.Scroll }

func (h *stubHost) UIQuery(id string) string { return h.ui.Query }

func (h *stubHost) Page() int {
	return h.page
}

func (h *stubHost) PageCount() int {
	return h.pages
}

func (h *stubHost) GotoPage(page int) error {
	h.page = page
	return nil
}

func (h *stubHost) GotoDocumentPoint(page int, x, y float64) error {
	h.page = page
	return nil
}

func (h *stubHost) Message() string {
	return h.message
}

func (h *stubHost) SetMessage(message string) {
	h.message = message
}

func (h *stubHost) RunCommand(command string) error {
	h.commands = append(h.commands, command)
	return nil
}

func (h *stubHost) Mode() string { return h.mode }

func (h *stubHost) Search(query string, backward bool) error {
	h.searchQuery = query
	h.searchBackward = backward
	return nil
}

func (h *stubHost) SearchQuery() string { return h.searchQuery }

func (h *stubHost) SearchMatchCount() int { return h.searchMatchCount }

func (h *stubHost) SearchMatchIndex() int { return h.searchMatchIndex }

func (h *stubHost) CurrentCount() string { return h.currentCount }

func (h *stubHost) PendingKeys() []string { return append([]string(nil), h.pendingKeys...) }

func (h *stubHost) ClearPendingKeys() {
	h.pendingKeys = nil
	h.currentCount = ""
}

func (h *stubHost) FitMode() string { return h.fitMode }

func (h *stubHost) SetFitMode(mode string) error {
	h.fitMode = mode
	return nil
}

func (h *stubHost) RenderMode() string { return h.renderMode }

func (h *stubHost) SetRenderMode(mode string) error {
	h.renderMode = mode
	return nil
}

func (h *stubHost) Zoom() float64 { return h.zoom }

func (h *stubHost) SetZoom(zoom float64) error {
	h.zoom = zoom
	return nil
}

func (h *stubHost) Rotation() float64 { return h.rotation }

func (h *stubHost) SetRotation(rotation float64) error {
	h.rotation = rotation
	return nil
}

func (h *stubHost) Fullscreen() bool { return h.fullscreen }

func (h *stubHost) SetFullscreen(fullscreen bool) error {
	h.fullscreen = fullscreen
	return nil
}

func (h *stubHost) CacheEntries() int { return h.cacheEntries }

func (h *stubHost) CachePending() int { return h.cachePending }

func (h *stubHost) CacheLimit() int { return h.cacheLimit }

func (h *stubHost) SetCacheLimit(limit int) error {
	h.cacheLimit = limit
	return nil
}

func (h *stubHost) ClearCache() { h.cacheCleared = true }

func TestOpenAppliesDocumentSpecificLuaConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.lua")
	if err := os.WriteFile(path, []byte(`
if onon.document.name == "special.pdf" then
  options.first_page_offset = false
end
`), 0o644); err != nil {
		t.Fatal(err)
	}

	rt, err := Open(path, filepath.Join(dir, "special.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	if rt.Config().FirstPageOffset {
		t.Fatalf("expected first_page_offset to be false for matching document.name")
	}
}

func TestSetDocumentReloadsDocumentSpecificLuaConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.lua")
	if err := os.WriteFile(path, []byte(`
if onon.document.name == "first.pdf" then
  options.page_gap_vertical = 11
end
if onon.document.name == "second.pdf" then
  options.page_gap_vertical = 22
end
`), 0o644); err != nil {
		t.Fatal(err)
	}

	rt, err := Open(path, filepath.Join(dir, "first.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	if got := rt.Config().PageGapVertical; got != 11 {
		t.Fatalf("expected first document config, got %d", got)
	}
	if err := rt.SetDocument(filepath.Join(dir, "second.pdf")); err != nil {
		t.Fatal(err)
	}
	if got := rt.Config().PageGapVertical; got != 22 {
		t.Fatalf("expected second document config, got %d", got)
	}
}

func TestSetDocumentPageCountIsAvailableDuringReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.lua")
	if err := os.WriteFile(path, []byte(`
if onon.document.page_count == 42 then
  options.page_gap_vertical = 42
end
`), 0o644); err != nil {
		t.Fatal(err)
	}

	rt, err := Open(path, filepath.Join(dir, "first.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	if err := rt.SetDocument(filepath.Join(dir, "second.pdf"), 42); err != nil {
		t.Fatal(err)
	}
	if got := rt.Config().PageGapVertical; got != 42 {
		t.Fatalf("expected Lua reload to see page_count, got %d", got)
	}
}

func TestOpenStoresAbsoluteDocumentPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.lua")
	want, err := filepath.Abs("doc.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`
if onon.document.path == `+strconv.Quote(want)+` then
  options.page_gap_vertical = 12
end
`), 0o644); err != nil {
		t.Fatal(err)
	}

	rt, err := Open(path, "doc.pdf")
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	if rt.docPath != want {
		t.Fatalf("expected absolute document path %q, got %q", want, rt.docPath)
	}
	if got := rt.Config().PageGapVertical; got != 12 {
		t.Fatalf("expected lua config to see absolute document path, got page_gap_vertical=%d", got)
	}
}

func TestLuaFunctionBindingMutatesConfigAtRuntime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.lua")
	if err := os.WriteFile(path, []byte(`
bind("h", function()
  options.page_gap_vertical = 10
end)
`), 0o644); err != nil {
		t.Fatal(err)
	}

	rt, err := Open(path, filepath.Join(dir, "doc.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	action := rt.Config().KeyBindings["h"]
	if action == "" {
		t.Fatal("expected key binding for h")
	}
	if handled, dirty, err := rt.RunAction(action); !handled || !dirty || err != nil {
		t.Fatalf("expected lua callback to run, handled=%v dirty=%v err=%v", handled, dirty, err)
	}
	if got := rt.Config().PageGapVertical; got != 10 {
		t.Fatalf("expected page_gap_vertical=10, got %d", got)
	}
	if got := rt.Config().PageGap; got != 10 {
		t.Fatalf("expected page_gap=10 mirror, got %d", got)
	}
}

func TestFailedReloadPreservesActiveConfigAndCallbacks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.lua")
	if err := os.WriteFile(path, []byte(`
options.page_gap_vertical = 7
bind("h", function()
  options.page_gap_vertical = 10
end)
`), 0o644); err != nil {
		t.Fatal(err)
	}

	rt, err := Open(path, filepath.Join(dir, "doc.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	action := rt.Config().KeyBindings["h"]

	if err := os.WriteFile(path, []byte("options.page_gap_vertical =\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := rt.Reload(); err == nil {
		t.Fatal("expected invalid Lua config to fail")
	}
	if got := rt.Config().PageGapVertical; got != 7 {
		t.Fatalf("failed reload changed page gap to %d", got)
	}
	if got := rt.Config().KeyBindings["h"]; got != action {
		t.Fatalf("failed reload changed binding to %q", got)
	}
	if handled, dirty, err := rt.RunAction(action); !handled || !dirty || err != nil {
		t.Fatalf("preserved callback failed: handled=%v dirty=%v err=%v", handled, dirty, err)
	}
	if got := rt.Config().PageGapVertical; got != 10 {
		t.Fatalf("preserved callback set page gap to %d", got)
	}
}

func TestFailedSetDocumentKeepsNewDocumentMetadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.lua")
	if err := os.WriteFile(path, []byte("options.page_gap_vertical = 7\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(dir, "first.pdf")
	rt, err := Open(path, first)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if err := os.WriteFile(path, []byte("options.page_gap_vertical =\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(dir, "second.pdf")
	if err := rt.SetDocument(second, 42); err == nil {
		t.Fatal("expected invalid document config to fail")
	}
	if rt.docPath != second || rt.docName != filepath.Base(second) || rt.docMeta.pageCount != 42 || !rt.docMeta.hasPages {
		t.Fatalf("failed document reload changed metadata: path=%q name=%q meta=%+v", rt.docPath, rt.docName, rt.docMeta)
	}
	if _, err := rt.Eval(`assert(onon.document.path == ` + strconv.Quote(second) + ` and onon.document.page_count == 42)`); err != nil {
		t.Fatalf("new Lua document metadata was not retained: %v", err)
	}
}

func TestActionValuesBindAndExecuteAgainstViewerHost(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.lua")
	if err := os.WriteFile(path, []byte(`
bind("J", onon.next_page)
bind("H", function()
  onon.next_page()
  onon.message("page " .. onon.page() .. "/" .. onon.page_count())
  onon.command(":fit width")
end)
`), 0o644); err != nil {
		t.Fatal(err)
	}

	rt, err := Open(path, filepath.Join(dir, "doc.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	if got := rt.Config().KeyBindings["J"]; got != "next_page" {
		t.Fatalf("expected J to bind next_page, got %q", got)
	}

	host := &stubHost{page: 3, pages: 12}
	rt.AttachHost(host)
	action := rt.Config().KeyBindings["H"]
	if handled, dirty, err := rt.RunAction(action); !handled || dirty || err != nil {
		t.Fatalf("expected lua callback to run cleanly, handled=%v dirty=%v err=%v", handled, dirty, err)
	}
	if len(host.actions) != 1 || host.actions[0] != "next_page" {
		t.Fatalf("expected next_page action, got %v", host.actions)
	}
	if host.message != "page 3/12" {
		t.Fatalf("expected message to be updated, got %q", host.message)
	}
	if len(host.commands) != 1 || host.commands[0] != ":fit width" {
		t.Fatalf("expected command to be forwarded, got %v", host.commands)
	}
	if handled, _, err := rt.RunAction("missing"); handled || err != nil {
		t.Fatalf("expected missing action to be ignored, handled=%v err=%v", handled, err)
	}
}

func TestEvalWorksWithoutConfigFile(t *testing.T) {
	dir := t.TempDir()
	rt, err := Open(filepath.Join(dir, "missing.lua"), filepath.Join(dir, "doc.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	host := &stubHost{}
	rt.AttachHost(host)
	if dirty, err := rt.Eval(`onon.message("inline lua works")`); dirty || err != nil {
		t.Fatalf("expected inline lua to run without config file: %v", err)
	}
	if host.message != "inline lua works" {
		t.Fatalf("expected host message from inline lua, got %q", host.message)
	}
}

func TestLuaVersionReportsBuildVersion(t *testing.T) {
	dir := t.TempDir()
	rt, err := Open(filepath.Join(dir, "missing.lua"), filepath.Join(dir, "doc.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	previous := Version
	Version = "v1.2.3"
	defer func() { Version = previous }()
	if _, err := rt.Eval(`assert(onon.version() == "v1.2.3", onon.version())`); err != nil {
		t.Fatal(err)
	}
}

func TestLuaRecentFilesReturnsLimitedList(t *testing.T) {
	setTestDataDir(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.lua")
	one := AbsoluteDocumentPath(filepath.Join(dir, "one.pdf"))
	two := AbsoluteDocumentPath(filepath.Join(dir, "two.pdf"))
	three := AbsoluteDocumentPath(filepath.Join(dir, "three.pdf"))
	if err := RecordRecentFile(one, 10); err != nil {
		t.Fatal(err)
	}
	if err := RecordRecentFile(two, 10); err != nil {
		t.Fatal(err)
	}
	if err := RecordRecentFile(three, 10); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`
local default_files = onon.recent_files()
local limited_files = onon.recent_files(2)
if #default_files == 3 then
  options.outline_initial_depth = 31
end
if #limited_files == 2 and limited_files[1] == `+strconv.Quote(three)+` and limited_files[2] == `+strconv.Quote(two)+` then
  options.outline_width_percent = 32
end
options.session_database = false
if #onon.recent_files(2) == 0 then
  options.outline_height_percent = 33
end
`), 0o644); err != nil {
		t.Fatal(err)
	}

	rt, err := Open(path, filepath.Join(dir, "doc.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	if got := rt.Config().OutlineInitialDepth; got != 31 {
		t.Fatalf("expected default recent files list, got outline_initial_depth=%d", got)
	}
	if got := rt.Config().OutlineWidthPercent; got != 32 {
		t.Fatalf("expected limited recent files list, got outline_width_percent=%d", got)
	}
	if got := rt.Config().OutlineHeightPercent; got != 33 {
		t.Fatalf("expected disabled session database to return no recents, got outline_height_percent=%d", got)
	}
}

func TestMouseInteractionOptions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.lua")
	if err := os.WriteFile(path, []byte(`
options.invert_scroll = { trackpad = true }
options.anti_aliasing = 4
options.render_oversample = 0.75
options.page_cache_size = 12
bind_mouse("right_down", onon.pan)
`), 0o644); err != nil {
		t.Fatal(err)
	}

	rt, err := Open(path, filepath.Join(dir, "doc.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	if got := rt.Config().MouseBindings["right_down"]; got != "pan" {
		t.Fatalf("expected right_down to bind pan, got %q", got)
	}
	if got := rt.Config().InvertScrollSources; got != SmoothInputTrackpad {
		t.Fatalf("expected only trackpad scrolling inverted, got %q", FormatSmoothInputSources(got))
	}
	if got := rt.Config().AntiAliasing; got != 4 {
		t.Fatalf("expected anti_aliasing=4, got %d", got)
	}
	if got := rt.Config().RenderOversample; got != 0.75 {
		t.Fatalf("expected render_oversample=0.75, got %.2f", got)
	}
	// page_cache_size is deprecated but must still be accepted.
}

func TestRuntimeOptionInspectionAndAssignment(t *testing.T) {
	rt, err := Open(filepath.Join(t.TempDir(), "missing.lua"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	if value, err := rt.OptionValue("scroll_step"); err != nil || value != "64" {
		t.Fatalf("OptionValue(scroll_step) = %q, %v", value, err)
	}
	if err := rt.SetOption("smooth_scroll", `{ mouse = true, trackpad = false, keyboard = true }`); err != nil {
		t.Fatal(err)
	}
	if cfg := rt.Config(); cfg.SmoothScrollSources != SmoothInputMouse|SmoothInputKeyboard {
		t.Fatalf("expected smooth_scroll source table assignment, got %q", FormatSmoothInputSources(cfg.SmoothScrollSources))
	}
	if err := rt.SetOption("smooth_scroll", `{ mouse = false }`); err != nil {
		t.Fatal(err)
	}
	if cfg := rt.Config(); cfg.SmoothScrollSources != SmoothInputKeyboard {
		t.Fatalf("expected partial smooth_scroll update, got %q", FormatSmoothInputSources(cfg.SmoothScrollSources))
	}
	if value, err := rt.OptionValue("smooth_scroll"); err != nil || value != `{ mouse = false, trackpad = false, keyboard = true }` {
		t.Fatalf("expected complete smooth_scroll table value, got %q, %v", value, err)
	}
	if err := rt.SetOption("scroll_step", "96"); err != nil {
		t.Fatal(err)
	}
	if rt.Config().ScrollStep != 96 {
		t.Fatalf("expected scroll_step=96, got %d", rt.Config().ScrollStep)
	}
	if value, err := rt.OptionValue("min_zoom"); err != nil || value != "0.5" {
		t.Fatalf("OptionValue(min_zoom) = %q, %v", value, err)
	}
	if err := rt.SetOption("min_zoom", "1.25"); err != nil {
		t.Fatal(err)
	}
	if err := rt.SetOption("max_zoom", "12"); err != nil {
		t.Fatal(err)
	}
	if cfg := rt.Config(); cfg.MinZoom != 1.25 || cfg.MaxZoom != 12 {
		t.Fatalf("expected zoom bounds 1.25..12, got %.2f..%.2f", cfg.MinZoom, cfg.MaxZoom)
	}
	if value, err := rt.OptionValue("pinch_sensitivity"); err != nil || value != "1" {
		t.Fatalf("OptionValue(pinch_sensitivity) = %q, %v", value, err)
	}
	if err := rt.SetOption("pinch_sensitivity", "0.5"); err != nil {
		t.Fatal(err)
	}
	if rt.Config().PinchSensitivity != 0.5 {
		t.Fatalf("expected pinch_sensitivity=0.5, got %v", rt.Config().PinchSensitivity)
	}
	if err := rt.SetOption("invert_scroll", "true"); err != nil {
		t.Fatal(err)
	}
	if value, err := rt.OptionValue("invert_scroll"); err != nil || value != `{ mouse = true, trackpad = true }` {
		t.Fatalf("expected invert_scroll=true to invert both wheel sources, got %q, %v", value, err)
	}
	if err := rt.SetOption("invert_scroll", `{ mouse = false }`); err != nil {
		t.Fatal(err)
	}
	if got := rt.Config().InvertScrollSources; got != SmoothInputTrackpad {
		t.Fatalf("expected partial invert_scroll update, got %q", FormatSmoothInputSources(got))
	}
	if err := rt.SetOption("invert_scroll", `{ keyboard = true }`); err == nil {
		t.Fatal("expected invert_scroll to reject the keyboard source")
	}
	if _, err := rt.OptionValue("invert_smooth_scroll"); err == nil {
		t.Fatal("expected removed invert_smooth_scroll option to be unknown")
	}
	if _, err := rt.OptionValue("natural_scroll"); err == nil {
		t.Fatal("expected removed natural_scroll option to be unknown")
	}
	if err := rt.SetOption("theme.background", "#102030"); err != nil {
		t.Fatal(err)
	}
	if got := rt.Config().Theme.Background; got != [3]uint8{16, 32, 48} {
		t.Fatalf("expected parsed background color, got %v", got)
	}
	if err := rt.SetOption("status_bar_left", `"{document} ready"`); err != nil {
		t.Fatal(err)
	}
	if got := rt.Config().StatusBarLeft; got != "{document} ready" {
		t.Fatalf("expected parsed status string, got %q", got)
	}
	if err := rt.ToggleOption("scroll_step"); err == nil {
		t.Fatal("expected toggling a non-boolean option to fail")
	}
}

func TestReferenceMetadataCoversRegisteredOptions(t *testing.T) {
	if err := ValidateReferenceMetadata(); err != nil {
		t.Fatal(err)
	}
	// The theme and its fields are documented by ThemeReferences.
	if got, want := len(OptionReferences())+len(ThemeReferences())+1, len(OptionNames()); got != want {
		t.Fatalf("expected %d option and theme references, got %d", want, got)
	}
	if len(LuaReferences()) == 0 {
		t.Fatal("expected Lua function references")
	}
}

func TestOutlineConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.lua")
	if err := os.WriteFile(path, []byte(`
options.outline_initial_depth = 2
options.outline_width_percent = 60
options.outline_height_percent = 75
bind("O", onon.outline)
`), 0o644); err != nil {
		t.Fatal(err)
	}

	rt, err := Open(path, filepath.Join(dir, "doc.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	cfg := rt.Config()
	if cfg.OutlineInitialDepth != 2 || cfg.OutlineWidthPercent != 60 || cfg.OutlineHeightPercent != 75 {
		t.Fatalf("unexpected outline config: %+v", cfg)
	}
	if got := cfg.KeyBindings["O"]; got != "outline" {
		t.Fatalf("expected outline binding, got %q", got)
	}
}

func TestCallingActionDuringConfigLoadFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.lua")
	if err := os.WriteFile(path, []byte(`bind("J", onon.next_page())`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Open(path, filepath.Join(dir, "doc.pdf"))
	if err == nil {
		t.Fatal("expected config load to fail")
	}
	if !strings.Contains(err.Error(), "cannot execute during config load") {
		t.Fatalf("expected config-load action error, got %v", err)
	}
}

func TestDocumentMetadataExposesFileFacts(t *testing.T) {
	dir := t.TempDir()
	docPath := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(docPath, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.lua")
	if err := os.WriteFile(path, []byte(`
if onon.document.exists and onon.document.extension == ".txt" and onon.document.size_bytes > 0 then
  options.page_gap_vertical = 12
end
`), 0o644); err != nil {
		t.Fatal(err)
	}

	rt, err := Open(path, docPath)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	if got := rt.Config().PageGapVertical; got != 12 {
		t.Fatalf("expected document metadata to drive config, got page_gap_vertical=%d", got)
	}
	if got := rt.Config().PageGap; got != 12 {
		t.Fatalf("expected mirrored page_gap=12, got %d", got)
	}
}

func TestExpandedLuaHostAPI(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.lua")
	if err := os.WriteFile(path, []byte(`
bind("X", function()
  local page = onon.page(9)
  local fit_mode = onon.fit_mode("width")
  local render_mode = onon.render_mode("single")
  local zoom = onon.zoom(1.75)
  local rotation = onon.rotation(180)
  local fullscreen = onon.fullscreen(false)
  onon.search("needle", true)
  local keys = onon.pending_keys()
  local cache_limit = onon.cache.limit(48)
  onon.cache.clear()
  if page ~= onon.page() or
     fit_mode ~= onon.fit_mode() or
     render_mode ~= onon.render_mode() or
     zoom ~= onon.zoom() or
     rotation ~= onon.rotation() or
     fullscreen ~= onon.fullscreen() or
     cache_limit ~= onon.cache.limit() then
    error("setter did not return the current value")
  end
  local message =
    tostring(page) .. ":" ..
    onon.mode() .. ":" ..
    fit_mode .. ":" ..
    render_mode .. ":" ..
    tostring(zoom) .. ":" ..
    tostring(rotation) .. ":" ..
    tostring(fullscreen) .. ":" ..
    tostring(cache_limit) .. ":" ..
    tostring(onon.search_match_index()) .. "/" .. tostring(onon.search_match_count()) .. ":" ..
    onon.current_count() .. ":" ..
    keys[1]
  if onon.message(message) ~= message or onon.message() ~= message then
    error("message setter did not return the current value")
  end
  onon.clear_pending_keys()
end)
`), 0o644); err != nil {
		t.Fatal(err)
	}

	rt, err := Open(path, filepath.Join(dir, "doc.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	host := &stubHost{
		mode:             "normal",
		searchMatchCount: 5,
		searchMatchIndex: 2,
		currentCount:     "12",
		pendingKeys:      []string{"g", "g"},
		fitMode:          "page",
		renderMode:       "continuous",
		zoom:             1.25,
		rotation:         90,
		fullscreen:       true,
		cacheEntries:     7,
		cachePending:     3,
		cacheLimit:       24,
	}
	rt.AttachHost(host)
	action := rt.Config().KeyBindings["X"]
	if handled, dirty, err := rt.RunAction(action); !handled || dirty || err != nil {
		t.Fatalf("expected expanded callback to run, handled=%v dirty=%v err=%v", handled, dirty, err)
	}
	if host.page != 9 || host.fitMode != "width" || host.renderMode != "single" {
		t.Fatalf("expected navigation/view setters to run, host=%+v", host)
	}
	if host.zoom != 1.75 || host.rotation != 180 || host.fullscreen {
		t.Fatalf("expected zoom/rotation/fullscreen/status updates, host=%+v", host)
	}
	if host.searchQuery != "needle" || !host.searchBackward {
		t.Fatalf("expected backward search to run, host=%+v", host)
	}
	if host.cacheLimit != 48 || !host.cacheCleared {
		t.Fatalf("expected cache controls to run, host=%+v", host)
	}
	if got := host.message; got != "9:normal:width:single:1.75:180:false:48:2/5:12:g" {
		t.Fatalf("unexpected message %q", got)
	}
	if len(host.pendingKeys) != 0 || host.currentCount != "" {
		t.Fatalf("expected pending input to clear, host=%+v", host)
	}
}

func TestPluginUIViewShowsMenuAndRunsSelectionCallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.lua")
	if err := os.WriteFile(path, []byte(`
bind("u", function()
  local view = onon.ui.create({
    title = "Open PDF",
    rows = { "a.pdf", "b.pdf" },
    selected = 2,
    on_select = function(index, value)
      onon.message(tostring(index) .. ":" .. value)
      onon.open(value)
    end,
    on_close = function()
      onon.message("closed")
    end,
  })
  view:show()
end)
`), 0o644); err != nil {
		t.Fatal(err)
	}

	rt, err := Open(path, filepath.Join(dir, "doc.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	host := &stubHost{}
	rt.AttachHost(host)
	if handled, dirty, err := rt.RunAction(rt.Config().KeyBindings["u"]); !handled || dirty || err != nil {
		t.Fatalf("expected ui callback to run, handled=%v dirty=%v err=%v", handled, dirty, err)
	}
	if !host.uiVisible || host.ui.Title != "Open PDF" || host.ui.Selected != 2 {
		t.Fatalf("unexpected ui state: %+v visible=%v", host.ui, host.uiVisible)
	}
	if got := strings.Join(uiListRowTexts(host.ui.Rows), ","); got != "a.pdf,b.pdf" {
		t.Fatalf("unexpected rows %q", got)
	}
	if host.ui.OnSelect == "" || host.ui.OnClose == "" {
		t.Fatalf("expected ui callbacks, got %+v", host.ui)
	}
	if err := rt.RunUISelect(host.ui.OnSelect, 2, "b.pdf"); err != nil {
		t.Fatal(err)
	}
	if host.message != "2:b.pdf" || host.opened != "b.pdf" {
		t.Fatalf("expected selection callback to update host, host=%+v", host)
	}
	if err := rt.RunUIClose(host.ui.OnClose); err != nil {
		t.Fatal(err)
	}
	if host.message != "closed" {
		t.Fatalf("expected close callback, got %q", host.message)
	}
}

func TestPluginUIViewSelectionCanOpenDocument(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.lua")
	second := filepath.Join(dir, "second.pdf")
	if err := os.WriteFile(path, []byte(`
if onon.document.name == "second.pdf" then
  onon.options.first_page_offset = false
end

bind("u", function()
  local view = onon.ui.create({
    rows = { `+strconv.Quote(second)+` },
    on_select = function(_, value)
      onon.open(value)
    end,
  })
  view:show()
end)
`), 0o644); err != nil {
		t.Fatal(err)
	}

	rt, err := Open(path, filepath.Join(dir, "first.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	host := &reloadingOpenHost{rt: rt}
	rt.AttachHost(host)
	if handled, _, err := rt.RunAction(rt.Config().KeyBindings["u"]); !handled || err != nil {
		t.Fatalf("expected ui callback to run, handled=%v err=%v", handled, err)
	}
	if err := rt.RunUISelect(host.ui.OnSelect, 1, second); err != nil {
		t.Fatal(err)
	}
	if host.opened != second {
		t.Fatalf("expected opened document %q, got %q", second, host.opened)
	}
	if rt.Config().FirstPageOffset {
		t.Fatalf("expected document-specific config reload after opening %q", second)
	}
}

func TestPluginUIViewHostControls(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.lua")
	if err := os.WriteFile(path, []byte(`
bind("u", function()
  local first = onon.ui.create({ id = "shared", rows = { "old" } })
  local view = onon.ui.create({ id = "shared", rows = { "old" } })
  assert(first.id ~= view.id)
  view:show()
  if view:visible() then
    view:set_rows({ "new", "items" })
    view:set_selected(2)
    view:close()
  end
end)
`), 0o644); err != nil {
		t.Fatal(err)
	}

	rt, err := Open(path, filepath.Join(dir, "doc.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	host := &stubHost{}
	rt.AttachHost(host)
	if handled, _, err := rt.RunAction(rt.Config().KeyBindings["u"]); !handled || err != nil {
		t.Fatalf("expected ui controls to run, handled=%v err=%v", handled, err)
	}
	if !host.uiClosed || host.uiVisible {
		t.Fatalf("expected ui to close, visible=%v closed=%v", host.uiVisible, host.uiClosed)
	}
	if host.ui.ID == "" {
		t.Fatal("expected an anonymous Lua view to receive a stable id")
	}
	if got := strings.Join(uiListRowTexts(host.ui.Rows), ","); got != "new,items" {
		t.Fatalf("unexpected rows %q", got)
	}
	if host.ui.Selected != 2 {
		t.Fatalf("unexpected selected %d", host.ui.Selected)
	}
}

func TestPluginUIViewShowDuringConfigLoadFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.lua")
	if err := os.WriteFile(path, []byte(`local view = onon.ui.create({ rows = { "x" } }); view:show()`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Open(path, filepath.Join(dir, "doc.pdf"))
	if err == nil {
		t.Fatal("expected config load to fail")
	}
	if !strings.Contains(err.Error(), "viewer host unavailable") {
		t.Fatalf("expected host unavailable error, got %v", err)
	}
}

func TestLuaUnbindRemovesDefaultBindings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.lua")
	if err := os.WriteFile(path, []byte(`
unbind("j")
unbind_mouse("middle_down")
unbind_mouse("ctrl_wheel_down")
`), 0o644); err != nil {
		t.Fatal(err)
	}

	rt, err := Open(path, filepath.Join(dir, "doc.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	cfg := rt.Config()
	if _, ok := cfg.KeyBindings["j"]; ok {
		t.Fatalf("expected j binding to be removed, got %q", cfg.KeyBindings["j"])
	}
	if _, ok := cfg.MouseBindings["middle_down"]; ok {
		t.Fatalf("expected middle_down mouse binding to be removed, got %q", cfg.MouseBindings["middle_down"])
	}
	if _, ok := cfg.MouseBindings["<C-wheel_down>"]; ok {
		t.Fatalf("expected ctrl_wheel_down alias to remove <C-wheel_down>, got %q", cfg.MouseBindings["<C-wheel_down>"])
	}
}

func TestLuaBindingsUseCanonicalNames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.lua")
	if err := os.WriteFile(path, []byte(`
unbind("<s-c-R>")
bind("<S-C-tab>", onon.quit)
bind("<Space>", onon.quit)
bind_mouse("<C-wheel-up>", onon.quit)
`), 0o644); err != nil {
		t.Fatal(err)
	}

	rt, err := Open(path, filepath.Join(dir, "doc.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	cfg := rt.Config()
	if action, ok := cfg.KeyBindings["<C-S-r>"]; ok {
		t.Fatalf("expected <s-c-R> to remove <C-S-r>, got %q", action)
	}
	for _, key := range []string{"<C-S-Tab>", "<Space>"} {
		if got := cfg.KeyBindings[key]; got != "quit" {
			t.Fatalf("expected %s bound to quit, got %q", key, got)
		}
	}
	if got := cfg.MouseBindings["<C-wheel_up>"]; got != "quit" {
		t.Fatalf("expected <C-wheel_up> bound to quit, got %q", got)
	}
}

func TestLuaBindingsRejectUnknownNames(t *testing.T) {
	tests := []struct {
		lua, want string
	}{
		{`bind("<C-Sapce>", onon.quit)`, `unknown key name "Sapce"`},
		{`bind("5x", onon.quit)`, `digits 1 to 9 start a count`},
		{`bind("'a", onon.quit)`, `' is used for marks`},
		{`bind('"', onon.quit)`, `" is used for marks`},
		{`unbind("<X-j>")`, `unknown modifier "X-"`},
		{`bind_mouse("left_dwn", onon.quit)`, `unknown mouse event "left_dwn"`},
		{`unbind_mouse("<C-left_down>")`, `unknown mouse event "<C-left_down>"`},
	}
	for _, tt := range tests {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.lua")
		if err := os.WriteFile(path, []byte(tt.lua), 0o644); err != nil {
			t.Fatal(err)
		}
		rt, err := Open(path, filepath.Join(dir, "doc.pdf"))
		if err == nil {
			rt.Close()
			t.Fatalf("expected %s to fail", tt.lua)
		}
		if !strings.Contains(err.Error(), tt.want) {
			t.Fatalf("expected %s to fail with %q, got %v", tt.lua, tt.want, err)
		}
	}
}

func TestDefaultBindingsAreValid(t *testing.T) {
	cfg := Default()
	goos := []string{""}
	for _, platform := range actions.Platforms() {
		goos = append(goos, platform.GOOS)
	}
	for _, system := range goos {
		for key := range actions.DefaultBindingsFor(system) {
			seq, err := keys.ParseSequence(key)
			if err != nil || keys.Format(seq) != key {
				t.Errorf("default key %q parses to %v, %v", key, seq, err)
				continue
			}
			if err := checkFirstKey(seq[0]); err != nil {
				t.Errorf("default key %q can never run: %v", key, err)
			}
		}
	}
	for event := range cfg.MouseBindings {
		if got, err := keys.NormalizeMouseEvent(event); err != nil || got != event {
			t.Errorf("default mouse event %q normalizes to %q, %v", event, got, err)
		}
	}
}

func TestLuaOptionTypeErrorsIncludeSettingName(t *testing.T) {
	tests := []struct {
		name    string
		lua     string
		wantErr string
	}{
		{name: "boolean", lua: `options.mouse_text_select = "yes"`, wantErr: "options.mouse_text_select: expected boolean"},
		{name: "input sources", lua: `options.invert_scroll = "yes"`, wantErr: "options.invert_scroll: expected table or boolean"},
		{name: "number", lua: `options.page_gap = true`, wantErr: "options.page_gap: expected number"},
		{name: "string", lua: `options.fit_mode = false`, wantErr: "options.fit_mode: expected string"},
		{name: "table", lua: `options.link_schemes = 10`, wantErr: "options.link_schemes: expected table"},
		{name: "theme colour", lua: `onon.theme.accent = 10`, wantErr: "onon.theme.accent: expected"},
		{name: "moved to theme", lua: `options.background = {1, 2, 3}`, wantErr: "options.background: background moved to onon.theme.background"},
		{name: "removed natural scroll", lua: `options.natural_scroll = true`, wantErr: "options.natural_scroll: unknown setting"},
		{name: "removed invert smooth scroll", lua: `options.invert_smooth_scroll = true`, wantErr: "options.invert_smooth_scroll: unknown setting"},
		{name: "unknown", lua: `options.no_such_setting = 1`, wantErr: "options.no_such_setting: unknown setting"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.lua")
			if err := os.WriteFile(path, []byte(tt.lua), 0o644); err != nil {
				t.Fatal(err)
			}

			rt, err := Open(path, filepath.Join(dir, "doc.pdf"))
			if rt != nil {
				rt.Close()
			}
			if err == nil {
				t.Fatal("expected config load to fail")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestLuaOptionNormalizationAndColorClamping(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.lua")
	if err := os.WriteFile(path, []byte(`
options.fit_mode = " WIDTH "
options.render_mode = "SINGLE"
options.anchor_position = "BOTTOM"
options.completion_max_items = 0
onon.theme.background = { -5, 128, 999 }
`), 0o644); err != nil {
		t.Fatal(err)
	}

	rt, err := Open(path, filepath.Join(dir, "doc.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	cfg := rt.Config()
	if cfg.FitMode != "width" {
		t.Fatalf("expected normalized fit mode width, got %q", cfg.FitMode)
	}
	if cfg.RenderMode != "single" {
		t.Fatalf("expected normalized render mode single, got %q", cfg.RenderMode)
	}
	if cfg.AnchorPosition != "bottom" {
		t.Fatalf("expected normalized anchor position bottom, got %q", cfg.AnchorPosition)
	}
	if cfg.CompletionMaxItems != 1 {
		t.Fatalf("expected completion_max_items to clamp to 1, got %d", cfg.CompletionMaxItems)
	}
	if cfg.Theme.Background != [3]uint8{0, 128, 255} {
		t.Fatalf("expected clamped background color, got %v", cfg.Theme.Background)
	}
}

func TestLoadReturnsConfigFromExplicitPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.lua")
	if err := os.WriteFile(path, []byte(`onon.options.status_bar_visible = false`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StatusBarVisible {
		t.Fatal("expected explicit config to hide status bar")
	}
	if cfg.ConfigPath != path {
		t.Fatalf("expected config path %q, got %q", path, cfg.ConfigPath)
	}
}

func TestLuaCanReadCurrentOptionValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.lua")
	if err := os.WriteFile(path, []byte(`
options.alt_colors = true
options.page_gap = 14
if options.alt_colors == true and options.page_gap == 14 then
  options.status_bar_left = "read"
end
`), 0o644); err != nil {
		t.Fatal(err)
	}

	rt, err := Open(path, filepath.Join(dir, "doc.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	cfg := rt.Config()
	if !cfg.AltColors || cfg.PageGap != 14 || cfg.StatusBarLeft != "read" {
		t.Fatalf("expected Lua to read option values and default message, got %+v", cfg)
	}
}

func TestLuaThemeColorsClampEveryColorField(t *testing.T) {
	var source strings.Builder
	want := map[string]string{}
	for i, field := range themeFields {
		if field.desc.kind != "color" {
			continue
		}
		fmt.Fprintf(&source, "onon.theme.%s = { -1, %d, 300 }\n", field.name, i)
		want[field.name] = FormatColor([3]uint8{0, uint8(i), 255})
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.lua")
	if err := os.WriteFile(path, []byte(source.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	rt, err := Open(path, filepath.Join(dir, "doc.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	for name, want := range want {
		if got, err := rt.OptionValue("theme." + name); err != nil || got != want {
			t.Errorf("theme.%s = %s, %v; want %s", name, got, err, want)
		}
	}
}

// --no-config starts from built-in defaults: neither the user's file nor the
// generated settings file is read.
func TestNoConfigIgnoresConfigurationFiles(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.lua")
	if err := os.WriteFile(configPath, []byte(`onon.options.scroll_off = 12`), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := OpenWithOptions(configPath, "", OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	if loaded.Config().ScrollOff != 12 {
		t.Fatalf("control case did not apply the config: ScrollOff = %d", loaded.Config().ScrollOff)
	}

	skipped, err := OpenWithOptions(configPath, "", OpenOptions{NoConfig: true})
	if err != nil {
		t.Fatal(err)
	}
	defer skipped.Close()
	if got, want := skipped.Config().ScrollOff, Default().ScrollOff; got != want {
		t.Fatalf("ScrollOff = %d, want the default %d", got, want)
	}
	if skipped.Config().ConfigPath != "" {
		t.Errorf("ConfigPath = %q, want empty", skipped.Config().ConfigPath)
	}
	// The Lua runtime still exists, so plugins and callbacks remain usable.
	if _, err := skipped.Eval(`assert(type(onon) == "table")`); err != nil {
		t.Fatalf("no-config runtime is unusable: %v", err)
	}
}

func TestLinkSchemesOption(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.lua")
	if err := os.WriteFile(path, []byte(`onon.options.link_schemes = {"HTTPS", "gemini:"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	rt, err := Open(path, "")
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	if got := rt.Config().LinkSchemes; !slices.Equal(got, []string{"https", "gemini"}) {
		t.Fatalf("Lua link_schemes = %q", got)
	}
	if err := rt.SetOption("link_schemes", `{"http", 'mailto'}`); err != nil {
		t.Fatal(err)
	}
	if value, err := rt.OptionValue("link_schemes"); err != nil || value != `{"http", "mailto"}` {
		t.Fatalf("OptionValue(link_schemes) = %q, %v", value, err)
	}
	if err := rt.SetOption("link_schemes", "http, https"); err != nil {
		t.Fatal(err)
	}
	if got := rt.Config().LinkSchemes; !slices.Equal(got, []string{"http", "https"}) {
		t.Fatalf(":set link_schemes = %q", got)
	}
}
