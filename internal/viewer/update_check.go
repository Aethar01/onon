package viewer

import (
	"context"
	"errors"
	"strings"

	"onon/internal/config"
	"onon/internal/update"
)

// checkForUpdates, installMethod and openReleasesPage are what
// :check-updates uses; tests replace them, so that no test reaches GitHub,
// depends on what is installed or opens a browser.
var (
	checkForUpdates  = update.Check
	installMethod    = func() string { return update.Method(config.InstallMethod) }
	openReleasesPage = (*App).OpenExternal
)

type updateCheck struct {
	result update.Result
	err    error
}

// checkUpdates asks GitHub for a newer release in the background;
// pollUpdateCheck reports what it finds.
func (a *App) checkUpdates(_ string) {
	if a.updateResult != nil {
		a.message = "already checking for updates"
		return
	}
	current := config.Version
	results := make(chan updateCheck, 1)
	a.updateResult = results
	a.message = "checking for updates"
	go func() {
		result, err := checkForUpdates(context.Background(), current)
		results <- updateCheck{result, err}
		a.wakeLoop()
	}()
}

func (a *App) pollUpdateCheck() {
	select {
	case check := <-a.updateResult:
		a.updateResult = nil
		a.reportUpdateCheck(check)
		a.pendingRedraw = true
	default:
	}
}

func (a *App) reportUpdateCheck(check updateCheck) {
	result, method := check.result, installMethod()
	switch {
	case errors.Is(check.err, update.ErrUnreleased):
		a.message = config.Version + " builds are not checked for updates"
	case check.err != nil:
		a.message = "update check failed: " + check.err.Error()
	case !result.Available:
		a.message = "onon " + config.Version + " is up to date"
	case method == "" && a.mode == modeNormal && a.activeUIView() == nil:
		// A download is upgraded from the releases page, so offer to open
		// it, unless that would interrupt what the user is doing.
		a.askPrompt("onon "+result.Latest+" is available, open the releases page? [Y/n]", "", func(answer string) {
			switch strings.ToLower(strings.TrimSpace(answer)) {
			case "", "y", "yes":
				if err := openReleasesPage(a, update.ReleasesURL); err != nil {
					a.message = "could not open " + update.ReleasesURL + ": " + err.Error()
				}
			}
		})
	default:
		a.message = "onon " + result.Latest + " is available: " + update.Instruction(method)
	}
}
