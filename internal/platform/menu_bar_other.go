//go:build !darwin

package platform

import "onon/internal/menubar"

// SetMenuBar shows menus as the system's menu bar. Only macOS has one.
func SetMenuBar(menus []menubar.Menu, bindings map[string]string) {}
