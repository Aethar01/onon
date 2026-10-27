package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	_ "embed"

	"onon/internal/config"
	"onon/internal/instance"
	"onon/internal/remote"
	"onon/internal/viewer"
)

//go:embed assets/onon.bmp
var iconBMP []byte

func main() {
	// ssh runs onon as its askpass to ask a question in the window that
	// started it.
	if code, ok := remote.RunAskpass(); ok {
		os.Exit(code)
	}
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	var cfgPath string
	var printVersion bool
	var verbose bool
	var noPlugins bool
	var noConfig bool
	var unique bool
	var gotoSpec string
	var command string
	flag.StringVar(&cfgPath, "config", "", "path to config.lua")
	flag.BoolVar(&printVersion, "v", false, "print version")
	flag.BoolVar(&verbose, "V", false, "enable verbose logging")
	flag.BoolVar(&noPlugins, "no-plugins", false, "disable Lua plugin loading")
	flag.BoolVar(&noConfig, "no-config", false, "start from built-in defaults, ignoring configuration files")
	flag.BoolVar(&unique, "unique", false, "reuse the window already showing this document, if there is one")
	flag.StringVar(&gotoSpec, "goto", "", "open at PAGE, or at X:Y points from the top-left corner of PAGE")
	flag.StringVar(&command, "command", "", "run a viewer command, as typed after ':'")
	flag.Parse()

	if printVersion {
		fmt.Println(config.Version)
		return nil
	}

	if noConfig && cfgPath != "" {
		return fmt.Errorf("--no-config and --config are contradictory; pass only one")
	}
	config.MigrateLegacyDirs() // before anything opens the session database

	target, err := instance.ParseTarget(gotoSpec)
	if err != nil {
		return err
	}
	// A --goto page is where the document should open, so it takes the place of
	// the old --page flag and likewise overrides a remembered session position.
	startPage := 1
	pageSet := target != nil
	if pageSet {
		startPage = target.Page
	}

	var docPath string
	if flag.NArg() == 0 {
		// Finder sends the documents to open in an Apple Event, not as
		// arguments. The window shows one document, so the first wins.
		if launched := viewer.LaunchDocuments(verbose); len(launched) > 0 {
			docPath = config.AbsoluteDocumentPath(launched[0])
		} else if recent := config.RecentFiles(1); len(recent) > 0 {
			docPath = recent[0]
			if session, ok := config.GetDocumentSession(docPath); !pageSet && ok {
				startPage = session.Page + 1
			}
		}
	} else {
		var err error
		docPath, err = explicitDocumentPath(flag.Arg(0))
		if err != nil {
			return err
		}
	}
	if verbose {
		log.Printf("verbose logging enabled")
		log.Printf("startup config=%q page=%d doc=%q", cfgPath, startPage, docPath)
	}

	// With --unique, a window already showing this document does the work and
	// this process exits. Any other document is a different instance.
	if unique && docPath != "" {
		handled, err := handOffToRunningInstance(docPath, target, command)
		if err != nil {
			return err
		}
		if handled {
			if verbose {
				log.Printf("handed %q to the running instance", docPath)
			}
			return nil
		}
	}

	runtime, err := config.OpenWithOptions(cfgPath, docPath, config.OpenOptions{
		Verbose:   verbose,
		NoPlugins: noPlugins,
		NoConfig:  noConfig,
	})
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	defer runtime.Close()
	defer config.CloseSessionDatabase() // after the viewer saves its session
	defer remote.Close()                // ends any SSH connections

	app, err := viewer.New(docPath, runtime, startPage-1, iconBMP, viewer.NewOptions{Verbose: verbose, StartPageExplicit: pageSet, LaunchArgs: launchArgs()})
	if err != nil {
		return fmt.Errorf("start viewer: %w", err)
	}
	defer app.Close()
	// This window answers for whichever document it is showing.
	app.EnableSingleInstance()
	defer app.CloseInstanceServer()
	// Only a point needs a deferred jump; a bare page is already the start page.
	if target != nil && target.HasPoint {
		app.QueueStartupJump(target.Page, target.X, target.Y, target.HasPoint)
	}
	// No window was showing this document, so this process runs the command
	// itself once its plugins are ready.
	app.QueueStartupCommand(command)

	return app.Run()
}

// handOffToRunningInstance asks the window already showing docPath to do the
// work. It reports whether the request was accepted; no such window is not an
// error, since the caller then opens one.
func handOffToRunningInstance(docPath string, target *instance.Request, command string) (bool, error) {
	address, err := instance.AddressFor(docPath)
	if err != nil {
		return false, nil
	}
	if _, err := instance.Send(address, instance.Request{Command: "ping"}); err != nil {
		return false, nil
	}
	request := instance.Request{Command: "open", Path: docPath}
	if target != nil {
		request.Page, request.X, request.Y, request.HasPoint = target.Page, target.X, target.Y, target.HasPoint
	}
	if _, err := instance.Send(address, request); err != nil {
		return false, fmt.Errorf("hand off to running onon: %w", err)
	}
	// The command runs after the open so it acts on the requested document.
	if command != "" {
		if _, err := instance.Send(address, instance.Request{Command: "run", Text: command}); err != nil {
			return false, fmt.Errorf("run %q: %w", command, err)
		}
	}
	return true, nil
}

// launchArgs returns the flags set for this process that a new window's
// process should share, so it loads the same configuration.
func launchArgs() []string {
	var args []string
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "config", "no-config", "no-plugins", "V":
			args = append(args, "-"+f.Name+"="+f.Value.String())
		}
	})
	return args
}

// explicitDocumentPath is the document named on the command line: a remote
// URI or scp-style host:path, which is fetched once the window is up, or a
// local file, which must exist.
func explicitDocumentPath(path string) (string, error) {
	if uri, ok, err := remote.FromArg(path); ok {
		return uri, err
	}
	path = config.AbsoluteDocumentPath(path)
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("open document: %w", err)
	}
	return path, nil
}
