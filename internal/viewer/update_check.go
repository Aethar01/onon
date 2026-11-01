package viewer

import (
	"context"
	"errors"

	"onon/internal/config"
	"onon/internal/update"
)

// checkForUpdates is what :check-updates checks with; tests replace it, so
// that no test reaches GitHub.
var checkForUpdates = update.Check

// checkUpdates asks GitHub for a newer release in the background;
// pollUpdateCheck reports what it finds.
func (a *App) checkUpdates(_ string) {
	if a.updateResult != nil {
		a.message = "already checking for updates"
		return
	}
	current, method := config.Version, update.Method(config.InstallMethod)
	results := make(chan string, 1)
	a.updateResult = results
	a.message = "checking for updates"
	go func() {
		msg, err := checkForUpdates(context.Background(), current, method)
		switch {
		case errors.Is(err, update.ErrUnreleased):
			msg = current + " builds are not checked for updates"
		case err != nil:
			msg = "update check failed: " + err.Error()
		}
		results <- msg
		a.wakeLoop()
	}()
}

func (a *App) pollUpdateCheck() {
	select {
	case msg := <-a.updateResult:
		a.message = msg
		a.updateResult = nil
		a.pendingRedraw = true
	default:
	}
}
