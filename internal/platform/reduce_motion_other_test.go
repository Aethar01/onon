//go:build !darwin && !windows

package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKDEAnimationSpeedZeroReducesMotion(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if kdeAnimationsOff() {
		t.Fatal("no kdeglobals should leave motion on")
	}
	write := func(s string) {
		if err := os.WriteFile(filepath.Join(dir, "kdeglobals"), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("[General]\nAnimationDurationFactor=0\n[KDE]\nAnimationDurationFactor=0.5\n")
	if kdeAnimationsOff() {
		t.Fatal("a slower animation speed is not off")
	}
	write("[KDE]\nSingleClick=false\nAnimationDurationFactor=0\n")
	if !kdeAnimationsOff() {
		t.Fatal("an instant animation speed should reduce motion")
	}
}
