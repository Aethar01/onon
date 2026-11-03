package viewer

import (
	"fmt"
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
	a.printSettings.copies = max(1, a.printSettings.copies)
	a.showRowList("print", "Print "+a.docName, a.printRows(nil), 50, 40, func(view *uiView) {
		view.searchable = false
		view.selected = selected
		view.onSelect = func(a *App, row uiRow) { a.choosePrintRow(view, row) }
	})
}

// refreshPrintDialog brings the open dialog's rows up to date, leaving its
// selection and a row being typed into as they are.
func (a *App) refreshPrintDialog() {
	if view := a.views.views["print"]; view != nil && view.visible {
		a.setUIViewRows(view, a.printRows(view))
	}
}

// printRows is the dialog's rows: its settings, as typed so far into a
// row of view being edited, then the button that prints them.
func (a *App) printRows(view *uiView) []uiRow {
	s := a.printSettingsTyped(view)
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
	note, _ := printNote(s, a.pageCount)
	rows := []uiRow{
		{text: "Printer", secondary: printer, value: "printer"},
		{text: "Pages", secondary: pages, value: "pages"},
		{text: "Copies", secondary: strconv.Itoa(s.copies), value: "copies"},
		{text: "Two-sided", secondary: sides, value: "sides"},
		{text: "Print", secondary: note, value: "print", button: true},
	}
	for i := range rows {
		rows[i].index = i
	}
	return rows
}

// printSettingsTyped is the dialog's settings with the text typed so far
// into its row being edited, if view has one, in place of that setting.
func (a *App) printSettingsTyped(view *uiView) printSettings {
	s := a.printSettings
	text, ok := view.rowEditText()
	if !ok {
		return s
	}
	switch view.rowEdit.row {
	case printPagesRow:
		s.pages = strings.ReplaceAll(text, " ", "")
	case printCopiesRow:
		if n, err := strconv.Atoi(strings.TrimSpace(text)); err == nil && n >= 1 {
			s.copies = n
		}
	}
	return s
}

// The rows of the print dialog typed into.
const (
	printPagesRow  = 1
	printCopiesRow = 2
)

// printNote is the dialog's note on the button: what the dialog's settings
// will print, such as 4 of 614 pages, or what is wrong with them, and
// whether they can be printed.
func (a *App) printNote() (string, bool) {
	return printNote(a.printSettings, a.pageCount)
}

func printNote(s printSettings, pageCount int) (string, bool) {
	pages, err := parsePageRanges(s.pages, pageCount)
	if err != nil {
		return err.Error(), false
	}
	note := fmt.Sprintf("%d of %d pages", len(pages), pageCount)
	if s.copies > 1 {
		note += fmt.Sprintf(", %d copies", s.copies)
	}
	return note, true
}

// choosePrintRow edits the setting on a row of the dialog, the pages and
// copies typed into the row itself, or prints, if the settings can be
// printed.
func (a *App) choosePrintRow(view *uiView, row uiRow) {
	s := &a.printSettings
	refresh := func(a *App, _ string) { a.refreshPrintDialog() }
	switch row.value {
	case "printer":
		a.pickPrinter(a.printers.names, a.refreshPrintDialog)
	case "pages":
		a.editRow(view, row.index, s.pages, uiRowEdit{
			hint: "such as 1-3,5", placeholder: "all", change: refresh, cancel: (*App).refreshPrintDialog,
			done: func(a *App, text string) {
				s.pages = strings.ReplaceAll(text, " ", "")
				a.refreshPrintDialog()
			},
		})
	case "copies":
		a.editRow(view, row.index, strconv.Itoa(max(1, s.copies)), uiRowEdit{
			change: refresh, cancel: (*App).refreshPrintDialog,
			done: func(a *App, text string) {
				if n, err := strconv.Atoi(strings.TrimSpace(text)); err == nil && n >= 1 {
					s.copies = n
				} else {
					a.message = fmt.Sprintf("copies must be a whole number from 1, not %q", text)
				}
				a.refreshPrintDialog()
			},
		})
	case "sides":
		for i, side := range printSides {
			if side.value == s.sides {
				s.sides = printSides[(i+1)%len(printSides)].value
				break
			}
		}
		a.refreshPrintDialog()
	case "print":
		if note, ok := a.printNote(); !ok {
			a.message = note
			return
		}
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

// parsePageRanges turns lp page ranges such as "1-3,5,7-" into 0-based
// pages of a document count pages long; empty means every page.
func parsePageRanges(ranges string, count int) ([]int, error) {
	if strings.TrimSpace(ranges) == "" {
		ranges = "1-"
	}
	var pages []int
	for part := range strings.SplitSeq(ranges, ",") {
		part = strings.TrimSpace(part)
		lo, hi, isRange := strings.Cut(part, "-")
		first, last := 1, count
		var err error
		if lo != "" || !isRange {
			if first, err = strconv.Atoi(lo); err != nil {
				return nil, fmt.Errorf("bad page range %q", part)
			}
		}
		switch {
		case !isRange:
			last = first
		case hi != "":
			if last, err = strconv.Atoi(hi); err != nil {
				return nil, fmt.Errorf("bad page range %q", part)
			}
		}
		if first < 1 || last < first || last > count {
			return nil, fmt.Errorf("page range %q is outside 1-%d", part, count)
		}
		for p := first; p <= last; p++ {
			pages = append(pages, p-1)
		}
	}
	return pages, nil
}

// startPrintJob is what runPrint prints with; tests replace it, so that
// no test reaches a real printer.
var startPrintJob = (*App).printJob

// runPrint starts printing the saved file in the background with settings s,
// or with lpArgs when given; pollPrintResult reports the outcome.
func (a *App) runPrint(s printSettings, lpArgs []string) {
	job, err := startPrintJob(a, s, lpArgs)
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
		a.refreshPrintDialog()
		a.pendingRedraw = true
	default:
	}
}
