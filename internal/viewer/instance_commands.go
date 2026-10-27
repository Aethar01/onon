package viewer

import (
	"fmt"
	"strings"

	"onon/internal/instance"
	"onon/internal/remote"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

// raiseWindow brings this window forward, so handing a document to a running
// instance surfaces it rather than silently changing a hidden window.
func (a *App) raiseWindow() {
	if a.window == nil {
		return
	}
	sdl.RaiseWindow(a.window)
}

// EnableSingleInstance makes this window reachable by other onon processes
// that open the same document. The address follows the open document, so
// switching files hands the old address back and claims the new one.
func (a *App) EnableSingleInstance() {
	a.singleInstance = true
	a.rebindInstanceServer()
}

// rebindInstanceServer points the listener at the current document. Losing the
// address is normal when another window already shows this file; that window
// answers for it and this one simply does not listen.
func (a *App) rebindInstanceServer() {
	if !a.singleInstance {
		return
	}
	// Work out what this document needs before touching the current listener,
	// so an unchanged document keeps the address it already holds.
	wanted := ""
	if a.docPath != "" {
		address, err := instance.AddressFor(a.docPath)
		if err != nil {
			a.logf("single instance unavailable: %v", err)
		} else {
			wanted = address
		}
	}
	if a.instanceServer != nil && a.instanceAddress == wanted {
		return
	}
	if a.instanceServer != nil {
		a.instanceServer.Close()
		a.instanceServer = nil
		a.instanceAddress = ""
	}
	if wanted == "" {
		return
	}
	address := wanted
	server, err := instance.Listen(address)
	if err != nil {
		a.logf("not listening for %q: %v", remote.Display(a.docPath), err)
		return
	}
	a.instanceServer = server
	a.instanceAddress = address
	a.logf("listening for instance commands on %q", address)
}

// CloseInstanceServer releases the address at shutdown.
func (a *App) CloseInstanceServer() {
	if a.instanceServer != nil {
		a.instanceServer.Close()
		a.instanceServer = nil
		a.instanceAddress = ""
	}
}

// pollInstanceCommands applies anything another process asked for. It runs on
// the main thread from the event loop, which is what makes it safe to touch
// viewer state directly. It reports whether anything changed.
func (a *App) pollInstanceCommands() bool {
	if a.instanceServer == nil {
		return false
	}
	changed := false
	for {
		select {
		case delivery := <-a.instanceServer.Deliveries():
			err := a.applyInstanceRequest(delivery.Request)
			delivery.Reply(err)
			if err != nil {
				a.logf("instance command %q failed: %v", delivery.Request.Command, err)
			} else {
				a.logf("instance command %q applied path=%q page=%d", delivery.Request.Command,
					remote.Display(delivery.Request.Path), delivery.Request.Page)
			}
			changed = true
		default:
			return changed
		}
	}
}

func (a *App) applyInstanceRequest(request instance.Request) error {
	switch request.Command {
	case "open":
		return a.applyInstanceOpen(request)
	case "run":
		return a.applyInstanceRun(request.Text)
	default:
		return fmt.Errorf("unknown command %q", request.Command)
	}
}

const unknownCommandPrefix = "unknown command: "

func (a *App) applyInstanceRun(command string) error {
	if command == "" {
		return fmt.Errorf("run: empty command")
	}
	// Clear first: the window keeps the last message indefinitely, so without
	// this a stale failure would be blamed on the command that follows it.
	previous := a.message
	a.message = ""
	// Built-in and plugin commands share this entry point, so an outside tool
	// can drive anything the command line can.
	if err := a.RunCommand(command); err != nil {
		return err
	}
	if strings.HasPrefix(a.message, unknownCommandPrefix) {
		return fmt.Errorf("%s", a.message)
	}
	if a.message == "" {
		// The command said nothing, so leave the window as it was.
		a.message = previous
	}
	return nil
}

func (a *App) applyInstanceOpen(request instance.Request) error {
	if request.Path != "" && request.Path != a.docPath {
		if err := a.Open(request.Path); err != nil {
			return err
		}
	}
	a.raiseWindow()
	if request.Page <= 0 {
		a.pendingRedraw = true
		return nil
	}
	// A document that is still loading has no page count yet, so the jump is
	// deferred until metrics arrive rather than being rejected.
	if a.pageCount == 0 {
		a.pendingInstanceJump = &instanceJump{page: request.Page, x: request.X, y: request.Y, hasPoint: request.HasPoint}
		return nil
	}
	return a.applyInstanceJump(instanceJump{page: request.Page, x: request.X, y: request.Y, hasPoint: request.HasPoint})
}

type instanceJump struct {
	page     int
	x, y     float64
	hasPoint bool
}

func (a *App) applyInstanceJump(jump instanceJump) error {
	if jump.hasPoint {
		// Command-line coordinates are measured from the page's top-left corner,
		// which is what an outside tool can know. The viewer works in the page's
		// own space, so shift by the box origin. This also matches synctex's
		// convention, so its output can be passed through unchanged.
		x, y := a.pageRelativeToDocument(jump.page, jump.x, jump.y)
		return a.GotoDocumentPoint(jump.page, x, y)
	}
	if jump.page < 1 || jump.page > a.pageCount {
		return fmt.Errorf("page %d out of range [1,%d]", jump.page, a.pageCount)
	}
	if err := a.GotoPage(jump.page); err != nil {
		return err
	}
	a.pendingRedraw = true
	return nil
}

// flushPendingInstanceJump applies a jump that arrived before the document was
// ready. It is called once page metrics exist.
func (a *App) flushPendingInstanceJump() {
	if a.pendingInstanceJump == nil || a.pageCount == 0 {
		return
	}
	jump := *a.pendingInstanceJump
	a.pendingInstanceJump = nil
	if err := a.applyInstanceJump(jump); err != nil {
		a.logf("deferred instance jump: %v", err)
	}
}

// pageRelativeToDocument converts a point measured from a page's top-left
// corner into the page's own coordinate space.
func (a *App) pageRelativeToDocument(page int, x, y float64) (float64, float64) {
	index := page - 1
	if index < 0 || index >= len(a.pageMetrics) {
		return x, y
	}
	bounds := a.pageMetrics[index].bounds
	return x + float64(bounds.X0), y + float64(bounds.Y0)
}

// QueueStartupCommand defers a --command until plugins have loaded, so asking
// a viewer that is not running yet to do something still works.
func (a *App) QueueStartupCommand(command string) {
	if command != "" {
		a.pendingInstanceCommand = command
	}
}

// flushPendingInstanceCommand runs a queued startup command. It is called once
// the document and plugins are ready.
func (a *App) flushPendingInstanceCommand() {
	if a.pendingInstanceCommand == "" {
		return
	}
	command := a.pendingInstanceCommand
	a.pendingInstanceCommand = ""
	if err := a.RunCommand(command); err != nil {
		a.logf("startup command %q: %v", command, err)
	}
}

// QueueStartupJump defers a --goto until the document has loaded. The viewer
// starts before page metrics exist, so the jump cannot be applied immediately.
func (a *App) QueueStartupJump(page int, x, y float64, hasPoint bool) {
	if page <= 0 {
		return
	}
	a.pendingInstanceJump = &instanceJump{page: page, x: x, y: y, hasPoint: hasPoint}
}
