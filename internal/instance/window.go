package instance

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// A window also listens on an address of its own, for documents to open
// in a new tab of whichever window was focused last, which it records.

// WindowAddressFor returns the address of the window in process pid.
func WindowAddressFor(pid int) (string, error) {
	dir, err := runtimeDir()
	if err != nil {
		return "", err
	}
	if dir == "" {
		return "", errors.New("no per-user runtime directory is available")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, fmt.Sprintf("window-%d.sock", pid)), nil
}

// LastWindow is the window focused last: its address, and the flags its
// process was started with, which load its configuration.
type LastWindow struct {
	Address string   `json:"address"`
	Args    []string `json:"args"`
}

// SameArgs reports whether the window's process was started with args,
// and so loaded the configuration a process started with them would.
func (w LastWindow) SameArgs(args []string) bool {
	return slices.Equal(w.Args, args) || len(w.Args) == 0 && len(args) == 0
}

func lastWindowPath() (string, error) {
	dir, err := runtimeDir()
	if err != nil {
		return "", err
	}
	if dir == "" {
		return "", errors.New("no per-user runtime directory is available")
	}
	return filepath.Join(dir, "last-window.json"), nil
}

// NoteLastWindow records w as the window focused last. The record is
// replaced whole, so a reader never sees half of one.
func NoteLastWindow(w LastWindow) error {
	path, err := lastWindowPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(w)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), "last-window-*.json")
	if err != nil {
		return err
	}
	_, writeErr := temp.Write(data)
	closeErr := temp.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		os.Remove(temp.Name())
		return err
	}
	if err := os.Rename(temp.Name(), path); err != nil {
		os.Remove(temp.Name())
		return err
	}
	return nil
}

// ReadLastWindow returns the window focused last, if one was recorded. It
// may since have closed, which sending to it finds out.
func ReadLastWindow() (LastWindow, bool) {
	path, err := lastWindowPath()
	if err != nil {
		return LastWindow{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return LastWindow{}, false
	}
	var w LastWindow
	if json.Unmarshal(data, &w) != nil || w.Address == "" {
		return LastWindow{}, false
	}
	return w, true
}
