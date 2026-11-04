package viewer

import (
	"testing"

	"onon/internal/actions"
)

func TestEveryRegisteredActionHasAHandler(t *testing.T) {
	registered := map[string]bool{}
	for _, name := range actions.Names() {
		registered[name] = true
		if builtinActions[name] == nil {
			t.Errorf("action %q has no handler", name)
		}
	}
	for name := range builtinActions {
		if !registered[name] {
			t.Errorf("handler %q is not in the actions registry", name)
		}
	}
}

func TestBuiltinPromptActionsEnterExpectedModes(t *testing.T) {
	tests := []struct {
		action     string
		wantMode   mode
		wantSearch searchMode
	}{
		{action: "command_mode", wantMode: modeCommand},
		{action: "goto_page_prompt", wantMode: modeGotoPage},
		{action: "search_prompt", wantMode: modeSearch, wantSearch: searchModeForward},
		{action: "search_prompt_backward", wantMode: modeSearch, wantSearch: searchModeBackward},
	}

	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			app := &App{tab: &tab{}, inputState: inputState{input: textInput{Value: "stale", Cursor: 5}, searchInput: searchModeBackward}}
			if err := app.runBuiltinAction(tt.action); err != nil {
				t.Fatal(err)
			}
			if app.mode != tt.wantMode || app.input.Value != "" || app.input.Cursor != 0 {
				t.Fatalf("expected clean input mode %v, got mode=%v input=%q cursor=%d", tt.wantMode, app.mode, app.input.Value, app.input.Cursor)
			}
			if tt.wantMode == modeSearch && app.searchInput != tt.wantSearch {
				t.Fatalf("expected search mode %v, got %v", tt.wantSearch, app.searchInput)
			}
		})
	}

	app := &App{tab: &tab{}}
	if err := app.runBuiltinAction("quit"); err != nil {
		t.Fatal(err)
	}
	if !app.quit {
		t.Fatal("expected builtin quit action to set quit flag")
	}
	if err := app.runBuiltinAction("not_an_action"); err == nil {
		t.Fatal("expected unknown builtin action to return an error")
	}
}
