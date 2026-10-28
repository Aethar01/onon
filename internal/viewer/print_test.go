package viewer

import (
	"slices"
	"testing"

	"onon/internal/config"

	"github.com/jupiterrider/purego-sdl3/sdl"
)

func TestParsePageRanges(t *testing.T) {
	for _, tc := range []struct {
		ranges string
		want   []int
	}{
		{"", []int{0, 1, 2, 3, 4}},
		{"1-3,5", []int{0, 1, 2, 4}},
		{"4-", []int{3, 4}},
		{"-2", []int{0, 1}},
		{" 2 , 2 ", []int{1, 1}},
	} {
		got, err := parsePageRanges(tc.ranges, 5)
		if err != nil || !slices.Equal(got, tc.want) {
			t.Errorf("parsePageRanges(%q) = %v, %v; want %v", tc.ranges, got, err, tc.want)
		}
	}
	for _, bad := range []string{"0", "6", "3-2", "a", "1,,2"} {
		if _, err := parsePageRanges(bad, 5); err == nil {
			t.Errorf("parsePageRanges(%q) accepted", bad)
		}
	}
}

// fakePrinting records the print jobs started in place of printing them,
// for the rest of the test.
func fakePrinting(t *testing.T) *[]printSettings {
	t.Helper()
	var jobs []printSettings
	real := startPrintJob
	startPrintJob = func(_ *App, s printSettings, _ []string) (func() (string, error), error) {
		jobs = append(jobs, s)
		return func() (string, error) { return "", nil }, nil
	}
	t.Cleanup(func() { startPrintJob = real })
	return &jobs
}

func TestPrintDialogSaysWhatWillPrint(t *testing.T) {
	jobs := fakePrinting(t)
	app := testStyleApp("bar")
	app.applyConfigState(config.Default()) // default key bindings
	app.docPath, app.docFile, app.docName = "/tmp/doc.pdf", "/tmp/doc.pdf", "doc.pdf"
	printRow := func() uiRow {
		view := app.activeModalUIView()
		return view.rows[len(view.rows)-1]
	}

	// The Enter that runs :print opens the dialog on its first row.
	app.runAction("command_mode")
	app.input.Set("print")
	app.handleSDLKeyDown(keyDown(sdl.KeycodeReturn, 0))
	if view := app.activeModalUIView(); view == nil || view.id != "print" || view.selected != 0 || len(*jobs) != 0 {
		t.Fatalf("after :print and Enter: view %v, jobs %v", view, *jobs)
	}
	if row := printRow(); row.secondary != "9 of 9 pages" {
		t.Fatalf("print row says %q", row.secondary)
	}

	// Pages out of range are said on the print row, and do not print.
	app.printSettings.pages = "1-3,12"
	app.showPrintDialog(4)
	if row := printRow(); row.secondary != `page range "12" is outside 1-9` {
		t.Fatalf("print row says %q", row.secondary)
	}
	app.activateUIView(app.activeModalUIView())
	if len(*jobs) != 0 || app.activeModalUIView() == nil {
		t.Fatalf("printed a bad range: jobs %v", *jobs)
	}

	app.printSettings.pages, app.printSettings.copies = "1-3,5", 2
	app.showPrintDialog(4)
	if row := printRow(); row.secondary != "4 of 9 pages, 2 copies" {
		t.Fatalf("print row says %q", row.secondary)
	}
	app.activateUIView(app.activeModalUIView())
	if len(*jobs) != 1 || (*jobs)[0].pages != "1-3,5" || app.activeModalUIView() != nil {
		t.Fatalf("Enter on Print: jobs %v, view %v", *jobs, app.activeModalUIView())
	}
}
