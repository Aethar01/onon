// Package remotetest stands a test binary in for ssh, serving SFTP from a
// temporary directory, so remote documents can be tested without a host.
package remotetest

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pkg/sftp"
)

const (
	homeEnv   = "ONON_FAKE_SSH_HOME"
	argsEnv   = "ONON_FAKE_SSH_ARGS"
	askEnv    = "ONON_FAKE_SSH_ASK"
	expectEnv = "ONON_FAKE_SSH_EXPECT"
)

// ServeIfFakeSSH serves SFTP on stdin and stdout and exits when the binary
// was started as the fake ssh. TestMain calls it first, then
// remote.RunAskpass, which the fake runs to ask its questions as ssh does.
func ServeIfFakeSSH() {
	home := os.Getenv(homeEnv)
	if home == "" {
		return
	}
	if out := os.Getenv(argsEnv); out != "" {
		_ = os.WriteFile(out, []byte(strings.Join(os.Args[1:], "\n")), 0o600)
	}
	if question := os.Getenv(askEnv); question != "" {
		answer, err := askpass(question)
		if err != nil || answer != os.Getenv(expectEnv) {
			fmt.Fprintln(os.Stderr, "me@host: Permission denied (password).")
			os.Exit(255)
		}
	}
	server, err := sftp.NewServer(stdio{}, sftp.WithServerWorkingDirectory(home))
	if err != nil {
		os.Exit(2)
	}
	_ = server.Serve()
	os.Exit(0)
}

type stdio struct{}

func (stdio) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdio) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (stdio) Close() error                { return os.Stdout.Close() }

// askpass asks question through SSH_ASKPASS, as ssh does without a terminal.
func askpass(question string) (string, error) {
	program := os.Getenv("SSH_ASKPASS")
	if program == "" || os.Getenv("SSH_ASKPASS_REQUIRE") != "force" {
		return "", errors.New("no askpass")
	}
	cmd := exec.Command(program, question)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, homeEnv+"=") {
			cmd.Env = append(cmd.Env, kv) // the askpass is not another fake ssh
		}
	}
	out, err := cmd.Output()
	return strings.TrimSuffix(string(out), "\n"), err
}

// RequireAnswer makes the fake ssh ask question before connecting and refuse
// to connect unless the answer is answer.
func RequireAnswer(t *testing.T, question, answer string) {
	t.Setenv(askEnv, question)
	t.Setenv(expectEnv, answer)
}

// FakeSSH returns a command to run in place of ssh for the rest of the
// test, the home directory it serves and the file it writes the arguments it
// was given to.
func FakeSSH(t *testing.T) (command, home, argsFile string) {
	t.Helper()
	home = t.TempDir()
	argsFile = filepath.Join(t.TempDir(), "args")
	command, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(homeEnv, home)
	t.Setenv(argsEnv, argsFile)
	return command, home, argsFile
}
