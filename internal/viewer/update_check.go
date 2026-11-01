package viewer

import (
	"context"
	"errors"
	"strings"

	"onon/internal/config"
	"onon/internal/update"
)

// checkForUpdates and openReleasesPage are what :check-updates uses; tests
// replace them, so that no test reaches GitHub or opens a browser.
var (
	checkForUpdates  = config.CheckUpdates
	openReleasesPage = (*App).OpenExternal
)

type updateCheck struct {
	info update.Info
	err  error
}

// checkUpdates asks GitHub for a newer release in the background;
// pollUpdateCheck reports what it finds.
func (a *App) checkUpdates(_ string) {
	if a.updateResult != nil {
		a.message = "already checking for updates"
		return
	}
	results := make(chan updateCheck, 1)
	a.updateResult = results
	a.message = "checking for updates"
	go func() {
		info, err := checkForUpdates(context.Background())
		results <- updateCheck{info, err}
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
	info := check.info
	switch {
	case errors.Is(check.err, update.ErrUnreleased):
		a.message = check.err.Error()
	case check.err != nil:
		a.message = "update check failed: " + check.err.Error()
	case !info.Available:
		a.message = "onon " + info.Current + " is up to date"
	case info.Method == "" && a.mode == modeNormal && a.activeUIView() == nil:
		// A download is upgraded from the releases page, so offer to open
		// it, unless that would interrupt what the user is doing.
		a.askPrompt("onon "+info.Latest+" is available, open the releases page? [Y/n]", "", func(answer string) {
			switch strings.ToLower(strings.TrimSpace(answer)) {
			case "", "y", "yes":
				if err := openReleasesPage(a, info.URL); err != nil {
					a.message = "could not open " + info.URL + ": " + err.Error()
				}
			}
		})
	default:
		a.message = "onon " + info.Latest + " is available: " + update.Instruction(info.Method)
	}
}
