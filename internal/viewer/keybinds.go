package viewer

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"onon/internal/config"
	"onon/internal/keys"
)

const newKeybindLabel = "+ New keybind..."

// The keybind menu lists the bindings under a button for a new one.
// Choosing a binding, or an action for a new one, captures the keys to
// bind it to.
type keybindMenuState struct {
	view      *uiView
	capturing bool
	// captured holds the keys pressed so far while capturing; they are
	// bound as one chord once no key follows within sequence_timeout_ms.
	captured        []string
	capturedAt      time.Time
	selectingAction bool
	captureAction   string
	// captureRow is the binding, or action, capture was started for, and
	// captureNew whether it makes a new binding, whatever is chosen while
	// the keys are pressed.
	captureRow keybindRow
	captureNew bool
	rows       []keybindRow
}

type keybindRow struct {
	key    string
	action string
}

func (a *App) toggleKeybindMenu() {
	if a.keybindMenu.view != nil && a.keybindMenu.view.visible {
		a.closeUIView(a.keybindMenu.view, false)
		return
	}
	a.closeAllUI()
	a.keybindMenu = keybindMenuState{view: a.newKeybindView()}
	a.refreshKeybindRows()
	a.showUIView(a.keybindMenu.view)
}

func (a *App) newKeybindView() *uiView {
	view := &uiView{
		id: "core:keybinds", owner: "core", modal: true, element: config.ElementPanel, place: percentPlace(76, 80),
		header:   func(a *App, _ *uiView, _ int) string { return a.keybindMenuHeader() },
		keyHook:  func(a *App, key keys.Key, repeat bool) bool { return a.keybindMenuKey(key, repeat) },
		onAction: func(a *App, _ *uiView, action string) bool { return a.keybindMenuAction(action) },
		onSelect: func(a *App, row uiRow) { a.chooseKeybindRow(row) },
	}
	return view
}

func (a *App) keybindMenuHeader() string {
	m := &a.keybindMenu
	if m.capturing {
		action := m.captureAction
		if action == "" {
			action = m.captureRow.action
		}
		if len(m.captured) > 0 {
			return action + ": " + strings.Join(m.captured, " ") + " …"
		}
		return "Press keys for " + action + " (Esc cancels)"
	}
	if m.selectingAction {
		return fmt.Sprintf("Select action (%d)", len(m.rows))
	}
	return fmt.Sprintf("Keybinds (%d)", len(m.rows))
}

// keybindMenuKey captures keys while capturing, Escape cancelling, and
// otherwise deletes the selected binding with Delete or Backspace.
func (a *App) keybindMenuKey(key keys.Key, repeat bool) bool {
	m := &a.keybindMenu
	if m.capturing {
		if repeat {
			return true
		}
		token := key.String()
		if token == "<Esc>" {
			m.capturing, m.captured, m.captureAction = false, nil, ""
			return true
		}
		m.captured = append(m.captured, token)
		m.capturedAt = time.Now()
		a.wakeAfter(time.Duration(a.config.SequenceTimeoutMS) * time.Millisecond)
		return true
	}
	if token := key.String(); !m.selectingAction && !repeat && (token == "<Del>" || token == "<BS>") {
		a.deleteSelectedKeybind()
		return true
	}
	return false
}

// keybindMenuAction goes back from choosing an action to the bindings on
// close, and closes the menu on its own toggle.
func (a *App) keybindMenuAction(action string) bool {
	m := &a.keybindMenu
	switch action {
	case "close", "keybinds":
		if m.selectingAction {
			m.selectingAction = false
			m.view.selected, m.view.scroll = 0, 0
			a.refreshKeybindRows()
			return true
		}
		if action == "keybinds" {
			a.closeUIView(m.view, false)
			return true
		}
	}
	return false
}

func (a *App) chooseKeybindRow(row uiRow) {
	m := &a.keybindMenu
	if m.capturing {
		return // the keys being pressed are for the row already chosen
	}
	if row.button {
		m.selectingAction = true
		m.view.selected, m.view.scroll = 0, 0
		a.refreshKeybindRows()
		return
	}
	if r, ok := a.selectedKeybind(); ok {
		if m.selectingAction {
			m.captureAction = r.action
		}
		m.capturing, m.captureRow, m.captureNew = true, r, m.selectingAction || r.key == ""
	}
}

// selectedKeybind is the binding, or action, of the selected row.
func (a *App) selectedKeybind() (keybindRow, bool) {
	m := &a.keybindMenu
	i := m.view.selected
	if !m.selectingAction {
		i-- // past the button
	}
	if i < 0 || i >= len(m.rows) {
		return keybindRow{}, false
	}
	return m.rows[i], true
}

func (a *App) refreshKeybindRows() {
	m := &a.keybindMenu
	var rows []uiRow
	if m.selectingAction {
		actions := config.Actions()
		if a.runtime != nil {
			actions = a.runtime.ActionNames()
		}
		sort.Strings(actions)
		m.rows = make([]keybindRow, 0, len(actions))
		for i, action := range actions {
			m.rows = append(m.rows, keybindRow{action: action})
			rows = append(rows, uiRow{index: i, text: action, value: action})
		}
	} else {
		m.rows = make([]keybindRow, 0, len(a.config.KeyBindings))
		for key, action := range a.config.KeyBindings {
			m.rows = append(m.rows, keybindRow{key: key, action: action})
		}
		sort.Slice(m.rows, func(i, j int) bool {
			if m.rows[i].action == m.rows[j].action {
				return m.rows[i].key < m.rows[j].key
			}
			return m.rows[i].action < m.rows[j].action
		})
		rows = append(rows, uiRow{index: 0, text: newKeybindLabel, button: true})
		for _, row := range m.rows {
			rows = append(rows, uiRow{index: len(rows), key: row.key, text: row.action, value: row.action})
		}
	}
	a.setUIViewRows(m.view, rows)
}

// captureDeadline is when the keys captured so far are bound, or the zero
// time when nothing is waiting.
func (a *App) captureDeadline() time.Time {
	if !a.keybindMenu.capturing || len(a.keybindMenu.captured) == 0 {
		return time.Time{}
	}
	return a.keybindMenu.capturedAt.Add(time.Duration(a.config.SequenceTimeoutMS) * time.Millisecond)
}

// finishKeybindCapture binds the captured chord once its pause has passed.
func (a *App) finishKeybindCapture(now time.Time) {
	if deadline := a.captureDeadline(); !deadline.IsZero() && !now.Before(deadline) {
		chord := strings.Join(a.keybindMenu.captured, "")
		a.keybindMenu.captured = nil
		a.rebindSelectedKey(chord)
		a.pendingRedraw = true
	}
}

func (a *App) deleteSelectedKeybind() {
	row, ok := a.selectedKeybind()
	if a.runtime == nil || !ok || row.key == "" {
		return
	}
	if err := a.runtime.UnbindKey(row.key); err != nil {
		a.message = err.Error()
	} else {
		a.message = fmt.Sprintf("unbound %s", row.key)
	}
	a.applyConfig(a.runtime.Config(), nil)
	a.refreshKeybindRows()
}

func (a *App) rebindSelectedKey(key string) {
	m := &a.keybindMenu
	row := m.captureRow
	if a.runtime == nil || m.view == nil || !m.capturing {
		return
	}
	action := row.action
	if m.captureAction != "" {
		action = m.captureAction
	}
	if strings.HasPrefix(action, "__lua_callback_") {
		a.message = "cannot persist callback keybind"
		m.capturing, m.captureAction = false, ""
		return
	}
	var err error
	if m.captureNew {
		err = a.runtime.SetKeyBinding(key, action)
	} else {
		err = a.runtime.RebindKey(row.key, key, action)
	}
	if err != nil {
		a.message = err.Error()
	} else {
		a.message = fmt.Sprintf("bound %s to %s", key, action)
	}
	a.applyConfig(a.runtime.Config(), nil)
	m.capturing, m.captureAction, m.selectingAction = false, "", false
	a.refreshKeybindRows()
}
