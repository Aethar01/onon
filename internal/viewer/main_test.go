package viewer

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"onon/internal/config"
	"onon/internal/remote"
	"onon/internal/remote/remotetest"
)

// TestMain runs the tests with the user's directories pointed at a
// temporary one, so no test reads or writes the real configuration or
// session database, and with printing refused, so no test reaches a real
// printer; fakePrinting records the jobs a test starts.
func TestMain(m *testing.M) {
	remotetest.ServeIfFakeSSH()
	if code, ok := remote.RunAskpass(); ok {
		os.Exit(code)
	}
	os.Exit(runIsolated(m))
}

func runIsolated(m *testing.M) int {
	dir, err := os.MkdirTemp("", "onon-viewer-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(dir)
	for _, name := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "APPDATA", "LOCALAPPDATA"} {
		os.Setenv(name, dir)
	}
	os.Unsetenv("XDG_CONFIG_DIRS")
	startPrintJob = func(*App, printSettings, []string) (func() (string, error), error) {
		return nil, errors.New("tests do not print")
	}
	defer config.CloseSessionDatabase()
	return m.Run()
}
