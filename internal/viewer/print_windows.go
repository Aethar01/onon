//go:build windows

package viewer

import (
	"fmt"
	"image"
	"runtime"
	"strconv"
	"strings"

	"onon/internal/mupdf"
	"onon/internal/winprint"
)

// printBandRows is how many rows of a page are rendered and sent at a time,
// keeping a page at printer resolution out of memory as a whole.
const printBandRows = 256

// printJob renders the document file to the printer, with the lp options
// in lpArgs when given and otherwise with s.
func (a *App) printJob(s printSettings, lpArgs []string) (func() (string, error), error) {
	if lpArgs != nil {
		var err error
		if s, err = parseLpArgs(lpArgs); err != nil {
			return nil, err
		}
	}
	path, opts, title, aa := a.docFile, a.openOptions(a.docPassword), a.docName, a.config.AntiAliasing
	return func() (string, error) {
		return "", printFile(path, opts, title, aa, s)
	}, nil
}

// parseLpArgs reads the lp options the print dialog sets.
func parseLpArgs(args []string) (printSettings, error) {
	s := printSettings{copies: 1}
	for i := 0; i < len(args); i++ {
		opt := args[i]
		if i+1 >= len(args) {
			return s, fmt.Errorf("%s needs a value", opt)
		}
		i++
		value := args[i]
		switch opt {
		case "-d":
			s.printer = value
		case "-P":
			s.pages = value
		case "-n":
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 {
				return s, fmt.Errorf("bad copies %q", value)
			}
			s.copies = n
		case "-o":
			sides, ok := strings.CutPrefix(value, "sides=")
			if !ok || sides != "one-sided" && sides != "two-sided-long-edge" && sides != "two-sided-short-edge" {
				return s, fmt.Errorf("unsupported option -o %s (Windows takes -d, -P, -n and -o sides=)", value)
			}
			if sides != "one-sided" {
				s.sides = sides
			}
		default:
			return s, fmt.Errorf("unsupported option %s (Windows takes -d, -P, -n and -o sides=)", opt)
		}
	}
	return s, nil
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

func printDuplex(sides string) winprint.Duplex {
	switch sides {
	case "two-sided-long-edge":
		return winprint.LongEdge
	case "two-sided-short-edge":
		return winprint.ShortEdge
	}
	return winprint.OneSided
}

// printFile opens its own copy of the saved file, so printing runs apart
// from the viewer, and prints each copy as its own job so that two-sided
// copies each start on a fresh sheet.
func printFile(path string, opts mupdf.OpenOptions, title string, aaLevel int, s printSettings) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	doc, err := mupdf.Open(path, opts)
	if err != nil {
		return err
	}
	defer doc.Close()
	pages, err := parsePageRanges(s.pages, doc.CachedPageCount())
	if err != nil {
		return err
	}
	r, err := doc.NewRenderer()
	if err != nil {
		return err
	}
	defer r.Close()
	for range max(1, s.copies) {
		job, err := winprint.Start(s.printer, title, printDuplex(s.sides))
		if err != nil {
			return err
		}
		for _, page := range pages {
			if err := printPage(job, doc, r, page, aaLevel); err != nil {
				job.Abort()
				return err
			}
		}
		if err := job.End(); err != nil {
			return err
		}
	}
	return nil
}

func printPage(job *winprint.Job, doc *mupdf.Document, r *mupdf.Renderer, page, aaLevel int) error {
	info, err := doc.PageInfo(page)
	if err != nil {
		return err
	}
	b := info.Bounds
	p, err := job.StartPage(float64(b.X1-b.X0), float64(b.Y1-b.Y0))
	if err != nil {
		return err
	}
	dev := mupdf.DeviceRect(b, p.Scale)
	for y := dev.Min.Y; y < dev.Max.Y; y += printBandRows {
		band := image.Rect(dev.Min.X, y, dev.Max.X, min(y+printBandRows, dev.Max.Y))
		rendered, err := r.Render(page, p.Scale, band, aaLevel)
		if err != nil {
			return err
		}
		err = p.Draw(rendered.Image, rendered.Y-dev.Min.Y, dev.Dy())
		rendered.Close()
		if err != nil {
			return err
		}
	}
	return p.End()
}

// findPrinterList asks the spooler for its printers and default printer.
func findPrinterList() printerList {
	names, defaultName, _ := winprint.Printers()
	return printerList{names: names, defaultName: defaultName}
}
