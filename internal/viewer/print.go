package viewer

import (
	"slices"
	"strconv"
	"strings"
)

// Printing starts from a small dialog for the printer, pages, copies and
// sides, or straight from :print with lp options. It goes through CUPS' lp
// on Linux and macOS, and on Windows renders the pages to the printer
// through GDI.

type printSettings struct {
	printer string
	pages   string // lp page ranges such as "1-3,5"; empty prints all
	copies  int
	sides   string // lp sides value; empty prints one-sided
}

var printSides = []struct{ value, label string }{
	{"", "off"},
	{"two-sided-long-edge", "long edge"},
	{"two-sided-short-edge", "short edge"},
}

// printDocument prints the document file: with lp options given, at once,
// and otherwise through the print dialog.
func (a *App) printDocument(args string) {
	if a.docPath == "" {
		a.message = "no document open"
		return
	}
	if fields := strings.Fields(args); len(fields) > 0 {
		a.runPrint(printSettings{}, fields)
		return
	}
	a.findPrinters()
	a.showPrintDialog(0)
}

// printerList is the printers found and the system's default one.
type printerList struct {
	names       []string
	defaultName string
}

// findPrinters looks for printers in the background, as that can take a
// second while the system looks for network printers.
func (a *App) findPrinters() {
	found := make(chan printerList, 1)
	a.printersFound = found
	go func() {
		found <- findPrinterList()
		a.wakeLoop()
	}()
}

func (a *App) showPrintDialog(selected int) {
	s := &a.printSettings
	s.copies = max(1, s.copies)
	sides := "off"
	for _, side := range printSides {
		if side.value == s.sides {
			sides = side.label
		}
	}
	printer := s.printer
	switch {
	case printer == "" && a.printersFound != nil:
		printer = "looking…"
	case printer == "":
		printer = "none found"
	}
	pages := s.pages
	if pages == "" {
		pages = "all"
	}
	rows := []uiRow{
		{text: "Printer", secondary: printer, value: "printer"},
		{text: "Pages", secondary: pages, value: "pages"},
		{text: "Copies", secondary: strconv.Itoa(s.copies), value: "copies"},
		{text: "Two-sided", secondary: sides, value: "sides"},
		{text: "Print", value: "print", button: true},
	}
	for i := range rows {
		rows[i].index = i
	}
	a.showRowList("print", "Print "+a.docName, rows, 50, 40, func(view *uiView) {
		view.searchable = false
		view.selected = selected
		view.onSelect = func(a *App, row uiRow) { a.choosePrintRow(row) }
	})
}

// choosePrintRow edits the setting on a dialog row, returning to the dialog
// with that row selected, or prints.
func (a *App) choosePrintRow(row uiRow) {
	s := &a.printSettings
	back := func() { a.showPrintDialog(row.index) }
	switch row.value {
	case "printer":
		a.pickPrinter(a.printers.names, back)
	case "pages":
		a.askPrompt("Pages (e.g. 1-3,5; blank for all)", s.pages, func(v string) {
			s.pages = strings.ReplaceAll(v, " ", "")
			back()
		})
	case "copies":
		a.askPrompt("Copies", strconv.Itoa(s.copies), func(v string) {
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 1 {
				s.copies = n
			}
			back()
		})
	case "sides":
		for i, side := range printSides {
			if side.value == s.sides {
				s.sides = printSides[(i+1)%len(printSides)].value
				break
			}
		}
		back()
	case "print":
		a.closeAllUI()
		a.runPrint(*s, nil)
	}
}

func (a *App) pickPrinter(printers []string, back func()) {
	if len(printers) == 0 {
		a.message = "no printers found"
		return
	}
	rows := make([]uiRow, len(printers))
	for i, p := range printers {
		rows[i] = uiRow{index: i, text: p, value: p}
	}
	a.showRowListOver("printers", "Printer", rows, 40, 40, func(view *uiView) {
		view.selected = max(0, slices.Index(printers, a.printSettings.printer))
		view.onSelect = func(a *App, row uiRow) {
			a.printSettings.printer = row.value
			a.closeUIView(view, false)
			back()
		}
	})
}

// runPrint starts printing the saved file in the background with settings s,
// or with lpArgs when given; pollPrintResult reports the outcome.
func (a *App) runPrint(s printSettings, lpArgs []string) {
	job, err := a.printJob(s, lpArgs)
	if err != nil {
		a.message = "print failed: " + err.Error()
		return
	}
	note := ""
	if a.unsaved {
		note = " (without unsaved edits)"
	}
	a.message = "printing " + a.docName + note
	results := make(chan string, 1)
	a.printResult = results
	go func() {
		msg, err := job()
		switch {
		case err != nil && msg != "":
			msg = "print failed: " + msg
		case err != nil:
			msg = "print failed: " + err.Error()
		case msg == "":
			msg = "sent " + a.docName + " to the printer"
		}
		results <- msg + note
		a.wakeLoop()
	}()
}

// pollPrintResult reports a finished print and fills in found printers.
func (a *App) pollPrintResult() {
	select {
	case msg := <-a.printResult:
		a.message = msg
		a.printResult = nil
		a.pendingRedraw = true
	default:
	}
	select {
	case found := <-a.printersFound:
		a.printersFound = nil
		a.printers = found
		if a.printSettings.printer == "" {
			a.printSettings.printer = found.defaultName
		}
		if view := a.activeUIView(); view != nil && view.id == "print" {
			a.showPrintDialog(view.selected)
		}
		a.pendingRedraw = true
	default:
	}
}
