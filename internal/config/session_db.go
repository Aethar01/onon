package config

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type DocumentSession struct {
	Page            int
	ScrollX         float64
	ScrollY         float64
	AnchorPage      int
	AnchorX         float64
	AnchorY         float64
	AnchorValid     bool
	Zoom            float64
	FitMode         string
	RenderMode      string
	Rotation        float64
	DualPage        bool
	FirstPageOffset bool
	StatusBarShown  bool
	AltColors       bool
}

type DocumentMark struct {
	Page        int
	ScrollX     float64
	ScrollY     float64
	AnchorPage  int
	AnchorX     float64
	AnchorY     float64
	AnchorValid bool
}

func SessionDatabasePath() string {
	dir := DataDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "session.sqlite")
}

func GetDocumentSession(path string) (DocumentSession, bool) {
	path = AbsoluteDocumentPath(path)
	if path == "" {
		return DocumentSession{}, false
	}
	db, err := openSessionDatabase()
	if err != nil || db == nil {
		return DocumentSession{}, false
	}
	var s DocumentSession
	err = db.QueryRow(`
		SELECT page, scroll_x, scroll_y, anchor_page, anchor_x, anchor_y, anchor_valid,
		       zoom, fit_mode, render_mode, rotation,
		       dual_page, first_page_offset, status_bar_shown, alt_colors
		FROM document_sessions
		WHERE path = ?
	`, path).Scan(
		&s.Page,
		&s.ScrollX,
		&s.ScrollY,
		&s.AnchorPage,
		&s.AnchorX,
		&s.AnchorY,
		&s.AnchorValid,
		&s.Zoom,
		&s.FitMode,
		&s.RenderMode,
		&s.Rotation,
		&s.DualPage,
		&s.FirstPageOffset,
		&s.StatusBarShown,
		&s.AltColors,
	)
	if errors.Is(err, sql.ErrNoRows) || err != nil {
		return DocumentSession{}, false
	}
	return s, true
}

func SetDocumentSession(path string, s DocumentSession) error {
	path = AbsoluteDocumentPath(path)
	if path == "" {
		return nil
	}
	db, err := openSessionDatabase()
	if err != nil {
		return err
	}
	if db == nil {
		return nil
	}
	_, err = db.Exec(`
		INSERT INTO document_sessions (
			path, page, scroll_x, scroll_y, anchor_page, anchor_x, anchor_y, anchor_valid,
			zoom, fit_mode, render_mode, rotation,
			dual_page, first_page_offset, status_bar_shown, alt_colors, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, unixepoch())
		ON CONFLICT(path) DO UPDATE SET
			page = excluded.page,
			scroll_x = excluded.scroll_x,
			scroll_y = excluded.scroll_y,
			anchor_page = excluded.anchor_page,
			anchor_x = excluded.anchor_x,
			anchor_y = excluded.anchor_y,
			anchor_valid = excluded.anchor_valid,
			zoom = excluded.zoom,
			fit_mode = excluded.fit_mode,
			render_mode = excluded.render_mode,
			rotation = excluded.rotation,
			dual_page = excluded.dual_page,
			first_page_offset = excluded.first_page_offset,
			status_bar_shown = excluded.status_bar_shown,
			alt_colors = excluded.alt_colors,
			updated_at = excluded.updated_at
	`, path, s.Page, s.ScrollX, s.ScrollY, s.AnchorPage, s.AnchorX, s.AnchorY, s.AnchorValid, s.Zoom, s.FitMode, s.RenderMode, s.Rotation, s.DualPage, s.FirstPageOffset, s.StatusBarShown, s.AltColors)
	return err
}

func RecordRecentFile(path string, maxEntries int) error {
	if maxEntries < 1 {
		return nil
	}
	path = AbsoluteDocumentPath(path)
	if path == "" {
		return nil
	}
	db, err := openSessionDatabase()
	if err != nil {
		return err
	}
	if db == nil {
		return nil
	}
	if _, err = db.Exec(`
		INSERT INTO recent_files (path, updated_at)
		VALUES (?, MAX(?, (SELECT COALESCE(MAX(updated_at), 0) + 1 FROM recent_files)))
		ON CONFLICT(path) DO UPDATE SET updated_at = excluded.updated_at
	`, path, time.Now().UnixNano()); err != nil {
		return err
	}
	_, err = db.Exec(`
		DELETE FROM recent_files
		WHERE path NOT IN (
			SELECT path
			FROM recent_files
			ORDER BY updated_at DESC, path ASC
			LIMIT ?
		)
	`, maxEntries)
	return err
}

// WindowTabs is a window's tabs as it closed: the documents open in them,
// in order, and the index of the one shown.
type WindowTabs struct {
	Paths  []string `json:"paths"`
	Active int      `json:"active"`
}

const windowTabsSetting = "window_tabs"

// SetLastWindowTabs records the tabs of the window that closed last, or
// forgets them when it had only one, which the recent files open again.
func SetLastWindowTabs(w WindowTabs) error {
	if len(w.Paths) < 2 {
		return setSetting(windowTabsSetting, "")
	}
	data, err := json.Marshal(w)
	if err != nil {
		return err
	}
	return setSetting(windowTabsSetting, string(data))
}

// LastWindowTabs returns the tabs SetLastWindowTabs recorded, if any.
func LastWindowTabs() (WindowTabs, bool) {
	value, err := getSetting(windowTabsSetting)
	if err != nil || value == "" {
		return WindowTabs{}, false
	}
	var w WindowTabs
	if json.Unmarshal([]byte(value), &w) != nil || len(w.Paths) < 2 {
		return WindowTabs{}, false
	}
	w.Active = min(max(w.Active, 0), len(w.Paths)-1)
	return w, true
}

func RecentFiles(limit int) []string {
	if limit < 1 {
		return nil
	}
	db, err := openSessionDatabase()
	if err != nil || db == nil {
		return nil
	}
	rows, err := db.Query(`
		SELECT path
		FROM recent_files
		ORDER BY updated_at DESC, path ASC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	paths := []string{}
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err == nil {
			paths = append(paths, path)
		}
	}
	return paths
}

// AddPromptHistory records entry as the newest in the named prompt history,
// keeping the newest limit distinct entries.
func AddPromptHistory(kind, entry string, limit int) error {
	if kind == "" || entry == "" || limit < 1 {
		return nil
	}
	db, err := openSessionDatabase()
	if err != nil || db == nil {
		return err
	}
	if _, err = db.Exec(`
		INSERT INTO prompt_history (kind, entry, updated_at)
		VALUES (?, ?, MAX(?, (SELECT COALESCE(MAX(updated_at), 0) + 1 FROM prompt_history WHERE kind = ?)))
		ON CONFLICT(kind, entry) DO UPDATE SET updated_at = excluded.updated_at
	`, kind, entry, time.Now().UnixNano(), kind); err != nil {
		return err
	}
	_, err = db.Exec(`
		DELETE FROM prompt_history
		WHERE kind = ? AND entry NOT IN (
			SELECT entry FROM prompt_history WHERE kind = ? ORDER BY updated_at DESC LIMIT ?
		)
	`, kind, kind, limit)
	return err
}

// PromptHistory returns up to limit entries of the named history, newest
// first.
func PromptHistory(kind string, limit int) []string {
	db, err := openSessionDatabase()
	if err != nil || db == nil || limit < 1 {
		return nil
	}
	rows, err := db.Query(`SELECT entry FROM prompt_history WHERE kind = ? ORDER BY updated_at DESC LIMIT ?`, kind, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var entries []string
	for rows.Next() {
		var entry string
		if rows.Scan(&entry) == nil {
			entries = append(entries, entry)
		}
	}
	return entries
}

func SetDocumentMark(path string, name string, mark DocumentMark) error {
	path = AbsoluteDocumentPath(path)
	if path == "" || name == "" {
		return nil
	}
	db, err := openSessionDatabase()
	if err != nil {
		return err
	}
	if db == nil {
		return nil
	}
	_, err = db.Exec(`
		INSERT INTO document_marks (
			path, name, page, scroll_x, scroll_y, anchor_page, anchor_x, anchor_y, anchor_valid, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(path, name) DO UPDATE SET
			page = excluded.page,
			scroll_x = excluded.scroll_x,
			scroll_y = excluded.scroll_y,
			anchor_page = excluded.anchor_page,
			anchor_x = excluded.anchor_x,
			anchor_y = excluded.anchor_y,
			anchor_valid = excluded.anchor_valid,
			updated_at = excluded.updated_at
	`, path, name, mark.Page, mark.ScrollX, mark.ScrollY, mark.AnchorPage, mark.AnchorX, mark.AnchorY, mark.AnchorValid, time.Now().UnixNano())
	return err
}

func GetDocumentMark(path string, name string) (DocumentMark, bool) {
	path = AbsoluteDocumentPath(path)
	if path == "" || name == "" {
		return DocumentMark{}, false
	}
	db, err := openSessionDatabase()
	if err != nil || db == nil {
		return DocumentMark{}, false
	}
	var mark DocumentMark
	err = db.QueryRow(`
		SELECT page, scroll_x, scroll_y, anchor_page, anchor_x, anchor_y, anchor_valid
		FROM document_marks
		WHERE path = ? AND name = ?
	`, path, name).Scan(&mark.Page, &mark.ScrollX, &mark.ScrollY, &mark.AnchorPage, &mark.AnchorX, &mark.AnchorY, &mark.AnchorValid)
	if errors.Is(err, sql.ErrNoRows) || err != nil {
		return DocumentMark{}, false
	}
	return mark, true
}

// getSetting returns a setting onon keeps for itself, or "" when it has
// none or there is no database.
func getSetting(name string) (string, error) {
	db, err := openSessionDatabase()
	if err != nil || db == nil {
		return "", err
	}
	var value string
	err = db.QueryRow(`SELECT value FROM settings WHERE name = ?`, name).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

func setSetting(name, value string) error {
	db, err := openSessionDatabase()
	if err != nil {
		return err
	}
	if db == nil {
		return errors.New("no data directory to keep settings in")
	}
	_, err = db.Exec(`
		INSERT INTO settings (name, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`, name, value, time.Now().UnixNano())
	return err
}

// storedKeyBindings returns the keybindings changed in the viewer: each
// key's action, or "" for a key unbound.
func storedKeyBindings() (map[string]string, error) {
	db, err := openSessionDatabase()
	if err != nil || db == nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT key, action FROM key_bindings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	bindings := map[string]string{}
	for rows.Next() {
		var key, action string
		if err := rows.Scan(&key, &action); err != nil {
			return nil, err
		}
		bindings[key] = action
	}
	return bindings, rows.Err()
}

// storeKeyBindings records keybindings changed in the viewer, an action of
// "" for a key unbound, together.
func storeKeyBindings(changes map[string]string) error {
	db, err := openSessionDatabase()
	if err != nil {
		return err
	}
	if db == nil {
		return errors.New("no data directory to keep keybindings in")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UnixNano()
	for key, action := range changes {
		if _, err := tx.Exec(`
			INSERT INTO key_bindings (key, action, updated_at)
			VALUES (?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET action = excluded.action, updated_at = excluded.updated_at
		`, key, action, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func deleteStoredKeyBindings(keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	db, err := openSessionDatabase()
	if err != nil || db == nil {
		return err
	}
	for _, key := range keys {
		if _, err := db.Exec(`DELETE FROM key_bindings WHERE key = ?`, key); err != nil {
			return err
		}
	}
	return nil
}

var sessionDB struct {
	sync.Mutex
	path string
	db   *sql.DB
}

// openSessionDatabase returns the shared connection to the session database,
// opening and migrating it on first use, or nil when there is no data
// directory. Callers must not close it.
func openSessionDatabase() (*sql.DB, error) {
	path := SessionDatabasePath()
	if path == "" {
		return nil, nil
	}
	sessionDB.Lock()
	defer sessionDB.Unlock()
	if sessionDB.db != nil && sessionDB.path == path {
		return sessionDB.db, nil
	}
	closeSessionDatabaseLocked() // the data directory moved
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // one writer; spares connections waiting on each other's locks
	if err := initSessionDatabase(db); err != nil {
		db.Close()
		return nil, err
	}
	sessionDB.path, sessionDB.db = path, db
	return db, nil
}

// CloseSessionDatabase closes the shared session database connection, if
// open; the next use reopens it.
func CloseSessionDatabase() {
	sessionDB.Lock()
	defer sessionDB.Unlock()
	closeSessionDatabaseLocked()
}

func closeSessionDatabaseLocked() {
	if sessionDB.db != nil {
		sessionDB.db.Close()
		sessionDB.path, sessionDB.db = "", nil
	}
}

func initSessionDatabase(db *sql.DB) error {
	if _, err := db.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		return err
	}
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS document_sessions (
			path TEXT PRIMARY KEY,
			page INTEGER NOT NULL,
			scroll_x REAL NOT NULL,
			scroll_y REAL NOT NULL,
			anchor_page INTEGER NOT NULL DEFAULT 0,
			anchor_x REAL NOT NULL DEFAULT 0,
			anchor_y REAL NOT NULL DEFAULT 0,
			anchor_valid INTEGER NOT NULL DEFAULT 0,
			zoom REAL NOT NULL,
			fit_mode TEXT NOT NULL,
			render_mode TEXT NOT NULL,
			rotation REAL NOT NULL,
			dual_page INTEGER NOT NULL,
			first_page_offset INTEGER NOT NULL,
			status_bar_shown INTEGER NOT NULL,
			alt_colors INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)
	`)
	if err != nil {
		return err
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS recent_files (
			path TEXT PRIMARY KEY,
			updated_at INTEGER NOT NULL
		)
	`); err != nil {
		return err
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS document_marks (
			path TEXT NOT NULL,
			name TEXT NOT NULL,
			page INTEGER NOT NULL,
			scroll_x REAL NOT NULL,
			scroll_y REAL NOT NULL,
			anchor_page INTEGER NOT NULL,
			anchor_x REAL NOT NULL,
			anchor_y REAL NOT NULL,
			anchor_valid INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY (path, name)
		)
	`); err != nil {
		return err
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS prompt_history (
			kind TEXT NOT NULL,
			entry TEXT NOT NULL,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY (kind, entry)
		)
	`); err != nil {
		return err
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS settings (
			name TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at INTEGER NOT NULL
		)
	`); err != nil {
		return err
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS key_bindings (
			key TEXT PRIMARY KEY,
			action TEXT NOT NULL,
			updated_at INTEGER NOT NULL
		)
	`); err != nil {
		return err
	}
	for _, stmt := range []string{
		`ALTER TABLE document_sessions ADD COLUMN anchor_page INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE document_sessions ADD COLUMN anchor_x REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE document_sessions ADD COLUMN anchor_y REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE document_sessions ADD COLUMN anchor_valid INTEGER NOT NULL DEFAULT 0`,
	} {
		if _, err := db.Exec(stmt); err != nil && !isDuplicateColumnError(err) {
			return err
		}
	}
	return initPluginStorage(db)
}

func isDuplicateColumnError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "duplicate column")
}
