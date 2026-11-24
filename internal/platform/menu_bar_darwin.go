//go:build darwin

package platform

/*
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
void ononSetMenuBar(const char *menus);
*/
import "C"

import (
	"encoding/json"
	"fmt"
	"unsafe"

	"onon/internal/keys"
	"onon/internal/menubar"
)

// menuBarActions is the action each item in the menu bar runs, by the
// item's tag less one; a tag of 0 is a system item.
var menuBarActions []string

type cocoaMenu struct {
	Title string      `json:"title"`
	Role  string      `json:"role,omitempty"`
	Items []cocoaItem `json:"items"`
}

type cocoaItem struct {
	Title string `json:"title,omitempty"`
	Role  string `json:"role,omitempty"`
	Tag   int    `json:"tag,omitempty"`
	Key   string `json:"key,omitempty"`
	Cmd   bool   `json:"cmd,omitempty"`
	Ctrl  bool   `json:"ctrl,omitempty"`
	Alt   bool   `json:"alt,omitempty"`
	Shift bool   `json:"shift,omitempty"`
}

// SetMenuBar installs menus as the macOS menu bar with shortcuts from bindings.
func SetMenuBar(menus []menubar.Menu, bindings map[string]string) {
	var actions []string
	cocoa := make([]cocoaMenu, 0, len(menus))
	for _, menu := range menus {
		items := make([]cocoaItem, 0, len(menu.Items))
		for _, item := range menu.Items {
			native := cocoaItem{Title: item.Title, Role: string(item.Role)}
			if item.Action != "" {
				actions = append(actions, item.Action)
				native.Tag = len(actions)
				if key, ok := menubar.Shortcut(bindings, item, keys.Cmd); ok {
					native.setKey(key)
				}
			}
			items = append(items, native)
		}
		cocoa = append(cocoa, cocoaMenu{Title: menu.Title, Role: string(menu.Role), Items: items})
	}
	encoded, err := json.Marshal(cocoa)
	if err != nil {
		return
	}
	menuBarActions = actions
	text := C.CString(string(encoded))
	defer C.free(unsafe.Pointer(text))
	C.ononSetMenuBar(text)
}

func (i *cocoaItem) setKey(key keys.Key) {
	name, ok := cocoaKeyEquivalent(key.Name)
	if !ok {
		return
	}
	i.Key = name
	i.Cmd = key.Mod&keys.Cmd != 0
	i.Ctrl = key.Mod&keys.Ctrl != 0
	i.Alt = key.Mod&keys.Alt != 0
	i.Shift = key.Mod&keys.Shift != 0
}

func cocoaKeyEquivalent(name string) (string, bool) {
	if len([]rune(name)) == 1 {
		return name, true
	}
	if r, ok := cocoaNamedKeys[name]; ok {
		return string(r), true
	}
	var n int
	if _, err := fmt.Sscanf(name, "F%d", &n); err == nil && n >= 1 && n <= 35 {
		return string(rune(0xF704 + n - 1)), true // NSF1FunctionKey onwards
	}
	return "", false
}

var cocoaNamedKeys = map[string]rune{
	"CR":    '\r',
	"Esc":   0x1b,
	"Tab":   '\t',
	"BS":    0x08,
	"Space": ' ',
	"Up":    0xF700,
	"Down":  0xF701,
	"Left":  0xF702,
	"Right": 0xF703,
	"Ins":   0xF727,
	"Del":   0xF728,
	"Home":  0xF729,
	"End":   0xF72B,
	"PgUp":  0xF72C,
	"PgDn":  0xF72D,
}

//export ononMenuBarChose
func ononMenuBarChose(tag C.int) {
	if i := int(tag) - 1; i >= 0 && i < len(menuBarActions) && hooks.OnAction != nil {
		hooks.OnAction(menuBarActions[i])
	}
}

//export ononMenuBarItemState
func ononMenuBarItemState(tag C.int) C.int {
	i := int(tag) - 1
	if i < 0 || i >= len(menuBarActions) || hooks.OnMenuItemState == nil {
		return 1
	}
	enabled, checked := hooks.OnMenuItemState(menuBarActions[i])
	state := C.int(0)
	if enabled {
		state |= 1
	}
	if checked {
		state |= 2
	}
	return state
}
