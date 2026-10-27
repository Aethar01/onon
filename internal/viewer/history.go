package viewer

import (
	"slices"
	"strings"

	"onon/internal/config"
	"onon/internal/remote"
)

// Prompt history lets Up and Down in the : and / prompts step through
// earlier entries that start with what was typed, like a shell.

type promptHistory struct {
	loaded  bool
	entries []string // newest first
	// While browsing, index is the entry shown and prefix what had been
	// typed; shown detects edits, which end browsing.
	index  int
	prefix string
	shown  string
}

func historyKind(m mode) string {
	switch m {
	case modeCommand:
		return "command"
	case modeSearch:
		return "search"
	}
	return ""
}

func (a *App) promptHistoryFor(m mode) *promptHistory {
	kind := historyKind(m)
	if kind == "" {
		return nil
	}
	if a.histories == nil {
		a.histories = map[string]*promptHistory{}
	}
	h := a.histories[kind]
	if h == nil {
		h = &promptHistory{index: -1}
		a.histories[kind] = h
	}
	if !h.loaded {
		h.loaded = true
		if a.config.SessionDatabase {
			h.entries = config.PromptHistory(kind, a.config.PromptHistoryMax)
		}
	}
	return h
}

// recordPromptHistory adds a submitted entry to the current prompt's history.
func (a *App) recordPromptHistory(m mode, entry string) {
	h := a.promptHistoryFor(m)
	if h == nil || entry == "" {
		return
	}
	if m == modeCommand {
		entry = remote.WithoutPasswords(entry)
	}
	h.entries = slices.DeleteFunc(h.entries, func(e string) bool { return e == entry })
	h.entries = slices.Insert(h.entries, 0, entry)
	h.entries = h.entries[:min(len(h.entries), a.config.PromptHistoryMax)]
	h.index = -1
	if a.config.SessionDatabase {
		if err := config.AddPromptHistory(historyKind(m), entry, a.config.PromptHistoryMax); err != nil {
			a.logf("save prompt history err=%v", err)
		}
	}
}

// stepPromptHistory shows the next older (delta 1) or newer (delta -1)
// entry matching the typed prefix; stepping past the newest restores what
// was typed. It reports whether the prompt has a history.
func (a *App) stepPromptHistory(delta int) bool {
	h := a.promptHistoryFor(a.mode)
	if h == nil {
		return false
	}
	if h.index < 0 || a.input.Value != h.shown {
		h.index, h.prefix = -1, a.input.Value
	}
	for i := h.index + delta; i >= -1 && i < len(h.entries); i += delta {
		if i == -1 {
			h.index = -1
			a.input.Set(h.prefix)
			return true
		}
		if strings.HasPrefix(h.entries[i], h.prefix) {
			h.index, h.shown = i, h.entries[i]
			a.input.Set(h.entries[i])
			return true
		}
	}
	return true
}
