//go:build !windows

package remote

import "os/exec"

func hideConsole(*exec.Cmd) {}
