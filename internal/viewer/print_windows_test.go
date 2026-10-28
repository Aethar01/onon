//go:build windows

package viewer

import "testing"

func TestParseLpArgsReadsDialogOptions(t *testing.T) {
	s := printSettings{printer: "Office", pages: "1-3,5", copies: 2, sides: "two-sided-long-edge"}
	got, err := parseLpArgs([]string{"-d", "Office", "-P", "1-3,5", "-n", "2", "-o", "sides=two-sided-long-edge"})
	if err != nil || got != s {
		t.Fatalf("parsed %+v, %v; want %+v", got, err, s)
	}
	if _, err := parseLpArgs([]string{"-o", "fit-to-page"}); err == nil {
		t.Fatal("accepted an option Windows cannot apply")
	}
	if _, err := parseLpArgs([]string{"-d"}); err == nil {
		t.Fatal("accepted -d without a printer")
	}
}
