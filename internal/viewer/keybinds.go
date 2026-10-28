package viewer

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"onon/internal/config"
	"onon/internal/keys"
)

const addKeyLabel = "Add a key…"

// The keybind menu lists every action with the keys bound to it. Choosing
// one opens its keys over the list, under a button for a new one:
// choosing a key captures the keys to bind in its place, and the button
// the keys of a new binding.
type keybindMenuState struct {
	view     *uiView // the actions
	keysView *uiView // the keys of action, over view
	action   string
	actions  []string // view's rows, in order
	keys     []string // action's keys, keysView's rows after its button
	// captured holds the keys pressed so far while capturing; they are
	// bound as one chord once no key follows within sequence_timeout_ms.
	capturing  bool
	captured   []string
	capturedAt time.Time
	// captureKey is the key capture began on, which the keys captured
	// replace, or empty for a new binding, whatever is chosen while they
	// are pressed.
	captureKey string
}

func (a *App) toggleKeybindMenu() {
	if m := &a.keybindMenu; m.view != nil && m.view.visible {
		a.closeUIView(m.keysView, false)
		a.closeUIView(m.view, false)
		return
	}
	a.closeAllUI()
	a.openKeybindMenu(false)
}

// openKeybindMenu opens the keybinding editor, over the menu open if over.
func (a *App) openKeybindMenu(over bool) {
	a.keybindMenu = keybindMenuState{view: a.newKeybindView()}
	a.keybindMenu.view.stack = over
	a.refreshKeybindRows()
	a.showUIView(a.keybindMenu.view)
}

func (a *App) newKeybindView() *uiView {
	return &uiView{
		id: "core:keybinds", owner: "core", modal: true, searchable: true, element: config.ElementPanel, place: menuPlace(76, 80),
		header:   func(a *App, _ *uiView, _ int) string { return fmt.Sprintf("Keybinds (%d)", len(a.keybindMenu.actions)) },
		onAction: func(a *App, _ *uiView, action string) bool { return a.keybindMenuAction(action) },
		onSelect: func(a *App, row uiRow) { a.openActionKeys(row.value) },
	}
}

// openActionKeys opens the keys of action over the list of actions.
func (a *App) openActionKeys(action string) {
	m := &a.keybindMenu
	m.action, m.capturing, m.captured = action, false, nil
	m.keysView = &uiView{
		id: "core:keybind-keys", owner: "core", modal: true, stack: true, element: config.ElementPanel, place: menuPlace(50, 50),
		header:   func(a *App, _ *uiView, _ int) string { return "Keys for " + a.keybindMenu.action },
		keyHook:  func(a *App, key keys.Key, repeat bool) bool { return a.keybindMenuKey(key, repeat) },
		onAction: func(a *App, _ *uiView, action string) bool { return a.keybindMenuAction(action) },
		onSelect: func(a *App, row uiRow) { a.chooseKeybindRow(row) },
		onClose:  func(a *App) { a.keybindMenu.capturing, a.keybindMenu.captured = false, nil },
	}
	a.refreshKeybindRows()
	a.showUIView(m.keysView)
}

// keybindMenuKey captures keys while capturing, Escape cancelling, and
// otherwise deletes the selected key with Delete or Backspace.
func (a *App) keybindMenuKey(key keys.Key, repeat bool) bool {
	m := &a.keybindMenu
	if m.capturing {
		if repeat {
			return true
		}
		token := key.String()
		if token == "<Esc>" {
			m.capturing, m.captured = false, nil
			a.refreshKeybindRows()
			return true
		}
		m.captured = append(m.captured, token)
		m.capturedAt = time.Now()
		a.wakeAfter(time.Duration(a.config.SequenceTimeoutMS) * time.Millisecond)
		a.refreshKeybindRows()
		return true
	}
	if token := key.String(); !repeat && (token == "<Del>" || token == "<BS>") {
		a.deleteSelectedKeybind()
		return true
	}
	return false
}

// keybindMenuAction closes the editor, the action's keys and the list
// both, on its own toggle.
func (a *App) keybindMenuAction(action string) bool {
	if action != "keybinds" {
		return false
	}
	a.closeUIView(a.keybindMenu.keysView, false)
	a.closeUIView(a.keybindMenu.view, false)
	return true
}

// chooseKeybindRow starts capturing the keys to bind: in place of the key
// chosen, or, from the button, for a new binding.
func (a *App) chooseKeybindRow(row uiRow) {
	m := &a.keybindMenu
	if m.capturing {
		return // the keys being pressed are for the row already chosen
	}
	m.capturing, m.captured, m.captureKey = true, nil, ""
	if !row.button {
		m.captureKey = row.value
	}
	a.refreshKeybindRows()
}

// selectedKey is the key of the selected row of the action's keys, if it
// is one and not the button.
func (a *App) selectedKey() (string, bool) {
	m := &a.keybindMenu
	if m.keysView == nil {
		return "", false
	}
	i := m.keysView.selected - 1 // past the button
	if i < 0 || i >= len(m.keys) {
		return "", false
	}
	return m.keys[i], true
}

// actionKeys is each action bound and its keys, sorted.
func (a *App) actionKeys() map[string][]string {
	bound := map[string][]string{}
	for key, action := range a.config.KeyBindings {
		bound[action] = append(bound[action], key)
	}
	for _, keys := range bound {
		sort.Strings(keys)
	}
	return bound
}

func (a *App) refreshKeybindRows() {
	m := &a.keybindMenu
	bound := a.actionKeys()
	if m.view != nil {
		actions := config.Actions()
		if a.runtime != nil {
			actions = a.runtime.ActionNames()
		}
		actions = slices.Clone(actions)
		for action := range bound {
			if !slices.Contains(actions, action) {
				actions = append(actions, action)
			}
		}
		sort.Strings(actions)
		m.actions = actions
		rows := make([]uiRow, len(actions))
		for i, action := range actions {
			rows[i] = uiRow{index: i, text: action, value: action, secondary: strings.Join(bound[action], "  ")}
		}
		a.setUIViewRows(m.view, rows)
	}
	if m.keysView != nil {
		m.keys = bound[m.action]
		rows := []uiRow{{index: 0, text: addKeyLabel, button: true}}
		for i, key := range m.keys {
			rows = append(rows, uiRow{index: i + 1, text: key, value: key})
		}
		if m.capturing {
			// The row being bound says so.
			i := 0
			if m.captureKey != "" {
				i = slices.Index(m.keys, m.captureKey) + 1
			}
			rows[i].secondary = "press keys, Esc cancels"
			if len(m.captured) > 0 {
				rows[i].secondary = strings.Join(m.captured, " ") + " …"
			}
		}
		a.setUIViewRows(m.keysView, rows)
	}
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
	key, ok := a.selectedKey()
	if a.runtime == nil || !ok {
		return
	}
	if err := a.runtime.UnbindKey(key); err != nil {
		a.message = err.Error()
	} else {
		a.message = fmt.Sprintf("unbound %s", key)
	}
	a.applyConfig(a.runtime.Config(), nil)
	a.refreshKeybindRows()
}

func (a *App) rebindSelectedKey(key string) {
	m := &a.keybindMenu
	if a.runtime == nil || m.keysView == nil || !m.capturing {
		return
	}
	m.capturing = false
	if strings.HasPrefix(m.action, "__lua_callback_") {
		a.message = "cannot persist callback keybind"
		a.refreshKeybindRows()
		return
	}
	var err error
	if m.captureKey == "" {
		err = a.runtime.SetKeyBinding(key, m.action)
	} else {
		err = a.runtime.RebindKey(m.captureKey, key, m.action)
	}
	if err != nil {
		a.message = err.Error()
	} else {
		a.message = fmt.Sprintf("bound %s to %s", key, m.action)
	}
	a.applyConfig(a.runtime.Config(), nil)
	a.refreshKeybindRows()
}
