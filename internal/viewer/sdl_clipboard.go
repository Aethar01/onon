package viewer

import (
	"fmt"

	"onon/internal/platform"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

var sdlSetClipboardText func(string) bool
var sdlGetClipboardText = sdl.GetClipboardText

func (a *App) GetClipboard() string {
	return sdlGetClipboardText()
}

func (a *App) SetClipboard(text string) error {
	return setSDLClipboardText(text)
}

func setSDLClipboardText(text string) error {
	if sdlSetClipboardText == nil {
		if err := loadSDLSetClipboardText(); err != nil {
			return err
		}
	}
	if !sdlSetClipboardText(text) {
		return sdlError("set clipboard text")
	}
	return nil
}

func loadSDLSetClipboardText() error {
	if err := platform.LoadSDLSymbol("SDL_SetClipboardText", &sdlSetClipboardText); err != nil {
		return fmt.Errorf("SDL_SetClipboardText unavailable: %w", err)
	}
	return nil
}
