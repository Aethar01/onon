package viewer

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"onon/internal/remote"
)

// inProcessWindows is whether a new window is a window of this process,
// as on macOS, rather than a process of its own; tests change it.
var inProcessWindows = runtime.GOOS == "darwin"

// startProcess starts a new window's process; tests replace it.
var startProcess = (*exec.Cmd).Start

// openInNewWindow opens path in a new window: on macOS a window of this
// process, so the system can gather the windows into tabs, and elsewhere a
// new onon process. A window or tab already showing the document is shown
// instead.
func (a *App) openInNewWindow(path string) error {
	if inProcessWindows {
		return a.openWindowHere(path)
	}
	return a.openInNewProcess(path)
}

// openWindowHere opens path in a new window of this process, or shows the
// window and tab that has it open.
func (a *App) openWindowHere(path string) error {
	page := 0
	if path != "" {
		resolved, at, err := a.resolveDocumentArg(path)
		if err != nil {
			return err
		}
		if a.showOpenDocument(resolved, at) {
			return nil
		}
		path, page = resolved, at
	}
	_, err := a.windowGroup().newWindow(a, path, page)
	return err
}

// openInNewProcess opens path in a new onon process. It runs with --unique, so
// a window already showing the document is raised instead of opening another.
// The executable is started directly: going through `open` would route the
// request back to this process.
func (a *App) openInNewProcess(path string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("open in new window: %w", err)
	}
	args := append(append([]string{}, a.launchArgs...), "--blank")
	if path != "" {
		args = append(a.launchArgs[:len(a.launchArgs):len(a.launchArgs)], "--unique", "--", path)
	}
	cmd := exec.Command(exe, args...)
	if a.verbose {
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	}
	if err := startProcess(cmd); err != nil {
		return fmt.Errorf("open in new window: %w", err)
	}
	a.logf("opened %q in a new window", remote.Display(path))
	go cmd.Wait() // reap it when it exits
	return nil
}
