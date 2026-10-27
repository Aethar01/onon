package viewer

import (
	"fmt"
	"os"
	"os/exec"

	"onon/internal/remote"
)

// startProcess starts a new window's process; tests replace it.
var startProcess = (*exec.Cmd).Start

// openInNewWindow opens path in a new onon process. It runs with --unique, so a
// window already showing the document is raised instead of opening another.
// The executable is started directly: going through `open` would route the
// request back to this process.
func (a *App) openInNewWindow(path string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("open in new window: %w", err)
	}
	args := append(append([]string{}, a.launchArgs...), "--unique", "--", path)
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
