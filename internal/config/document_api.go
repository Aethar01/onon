package config

// DocumentMetadata contains format-independent document information.
type DocumentMetadata struct {
	Format           string
	Encryption       string
	Title            string
	Author           string
	Subject          string
	Keywords         string
	Creator          string
	Producer         string
	CreationDate     string
	ModificationDate string
}

type DocumentPoint struct {
	X float64
	Y float64
}

type DocumentRect struct {
	X0 float64
	Y0 float64
	X1 float64
	Y1 float64
}

type DocumentOutlineItem struct {
	Title    string
	URI      string
	External bool
	Page     int
	X        *float64
	Y        *float64
	Children []DocumentOutlineItem
}

type DocumentPageInfo struct {
	Page   int
	Label  string
	Width  float64
	Height float64
	Bounds DocumentRect
}

// DocumentSelection describes the viewer's current text selection.
type DocumentSelection struct {
	Active bool
	Text   string
	Pages  []DocumentSelectionPage // in page order
}

type DocumentSelectionPage struct {
	Page  int
	Quads []DocumentRect
}

type DocumentPageLink struct {
	Bounds   DocumentRect
	URI      string
	External bool
	Page     int
	X        *float64
	Y        *float64
}

// These interfaces are optional extensions to Host so non-viewer hosts do not
// need to implement PDF inspection.
type DocumentMetadataHost interface {
	Metadata() (DocumentMetadata, error)
}

type DocumentOutlineHost interface {
	Outline() ([]DocumentOutlineItem, error)
}

type DocumentPageInfoHost interface {
	PageInfo(page int) (DocumentPageInfo, error)
}

type DocumentSelectionHost interface {
	Selection() (DocumentSelection, error)
}

// DocumentBinder is an optional extension to Host for a viewer showing
// one of several documents: what CurrentDocumentHost returns answers the
// document calls of a plugin's job, which runs on after another document
// is shown, for the document shown when it started.
type DocumentBinder interface {
	CurrentDocumentHost() any
}

// documentHost is what answers document calls asked for now.
func (r *Runtime) documentHost() any {
	if binder, ok := r.host.(DocumentBinder); ok {
		return binder.CurrentDocumentHost()
	}
	return r.host
}

type DocumentPageTextHost interface {
	PageText(page int) (string, error)
}

type DocumentPageLinksHost interface {
	PageLinks(page int) ([]DocumentPageLink, error)
}
