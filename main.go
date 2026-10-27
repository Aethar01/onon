package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

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
	// An argument that may be a scheme from config.lua, such as arxiv:id,
	// waits until the configuration that would register it is loaded.
	var schemeArg string
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
	} else if mayBeScheme(flag.Arg(0)) {
		schemeArg = flag.Arg(0)
	} else {
		var page int
		docPath, page, err = documentArg(flag.Arg(0), nil)
		if err != nil {
			return err
		}
		startPage, pageSet, target = fragmentPage(page, startPage, pageSet, target)
	}
	if verbose {
		log.Printf("verbose logging enabled")
		log.Printf("startup config=%q page=%d doc=%q", cfgPath, startPage, docPath)
	}

	// With --unique, a window already showing this document does the work and
	// this process exits. Any other document is a different instance.
	handOff := func() (bool, error) {
		if !unique || docPath == "" {
			return false, nil
		}
		handled, err := handOffToRunningInstance(docPath, target, command)
		if handled && verbose {
			log.Printf("handed %q to the running instance", remote.Display(docPath))
		}
		return handled, err
	}
	if handled, err := handOff(); handled || err != nil {
		return err
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
	if schemeArg != "" {
		var page int
		if docPath, page, err = documentArg(schemeArg, runtime); err != nil {
			return err
		}
		startPage, pageSet, target = fragmentPage(page, startPage, pageSet, target)
		if verbose {
			log.Printf("resolved %q to %q", schemeArg, remote.Display(docPath))
		}
		if handled, err := handOff(); handled || err != nil {
			return err
		}
		if err := runtime.SetDocument(docPath); err != nil {
			return fmt.Errorf("load config: %w", err)
		}
	}
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

// mayBeScheme reports whether arg could start with a scheme registered in
// config.lua: name:rest, where name is not a built-in scheme and arg is not
// a local file. scp-style host:path looks the same, and is told apart once
// the configuration is loaded.
func mayBeScheme(arg string) bool {
	name, _, ok := strings.Cut(arg, ":")
	if !ok || len(name) < 2 || strings.ContainsAny(name, `/\@[`) || remote.IsURI(arg) {
		return false
	}
	_, err := os.Lstat(arg)
	return err != nil
}

// documentArg resolves a document argument: through a scheme registered in
// rt's configuration, if any, then taking off a #page=N, which it returns.
func documentArg(arg string, rt *config.Runtime) (string, int, error) {
	arg, _, err := rt.ResolveScheme(arg)
	if err != nil {
		return "", 0, err
	}
	arg, page := remote.PageFragment(arg)
	path, err := explicitDocumentPath(arg)
	return path, page, err
}

// fragmentPage starts at a #page=N page unless --goto gave a page.
func fragmentPage(page, startPage int, pageSet bool, target *instance.Request) (int, bool, *instance.Request) {
	if page == 0 || pageSet {
		return startPage, pageSet, target
	}
	return page, true, &instance.Request{Page: page}
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
