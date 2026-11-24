//go:build !darwin && !windows

package platform

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ReducesMotion reports whether the desktop has turned animations off:
// GNOME's enable-animations, which other GTK desktops share, or KDE's
// animation speed set to instant.
func ReducesMotion() bool {
	return gnomeAnimationsOff() || kdeAnimationsOff()
}

func gnomeAnimationsOff() bool {
	if _, err := exec.LookPath("gsettings"); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "gsettings", "get", "org.gnome.desktop.interface", "enable-animations").Output()
	return err == nil && strings.TrimSpace(string(out)) == "false"
}

func kdeAnimationsOff() bool {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return false
		}
		dir = filepath.Join(home, ".config")
	}
	f, err := os.Open(filepath.Join(dir, "kdeglobals"))
	if err != nil {
		return false
	}
	defer f.Close()
	section := ""
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") {
			section = line
			continue
		}
		if key, value, ok := strings.Cut(line, "="); ok && section == "[KDE]" && strings.TrimSpace(key) == "AnimationDurationFactor" {
			return strings.TrimSpace(value) == "0"
		}
	}
	return false
}
