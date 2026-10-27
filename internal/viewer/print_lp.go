//go:build !windows

package viewer

import (
	"os/exec"
	"strconv"
	"strings"
)

func (s printSettings) lpArgs() []string {
	var args []string
	if s.printer != "" {
		args = append(args, "-d", s.printer)
	}
	if s.pages != "" {
		args = append(args, "-P", s.pages)
	}
	if s.copies > 1 {
		args = append(args, "-n", strconv.Itoa(s.copies))
	}
	if s.sides != "" {
		args = append(args, "-o", "sides="+s.sides)
	}
	return args
}

// printJob runs lp on the document file, with lpArgs when given and
// otherwise with options for s.
func (a *App) printJob(s printSettings, lpArgs []string) (func() (string, error), error) {
	if lpArgs == nil {
		lpArgs = s.lpArgs()
	}
	cmd := printCommand(a.docFile, lpArgs)
	return func() (string, error) {
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}, nil
}

func printCommand(path string, args []string) *exec.Cmd {
	return exec.Command("lp", append(args, "--", path)...)
}

// findPrinterList asks CUPS for its printers and default destination.
func findPrinterList() printerList {
	names := listPrinters()
	return printerList{names: names, defaultName: defaultPrinter(names)}
}

func listPrinters() []string {
	out, err := exec.Command("lpstat", "-e").Output()
	if err != nil {
		return nil
	}
	return strings.Fields(string(out))
}

// defaultPrinter is CUPS' default destination, or the first printer.
func defaultPrinter(printers []string) string {
	out, _ := exec.Command("lpstat", "-d").Output()
	if name, ok := parseDefaultPrinter(string(out)); ok {
		return name
	}
	if len(printers) > 0 {
		return printers[0]
	}
	return ""
}

func parseDefaultPrinter(lpstat string) (string, bool) {
	_, name, ok := strings.Cut(strings.TrimSpace(lpstat), "system default destination: ")
	return strings.TrimSpace(name), ok && name != ""
}
