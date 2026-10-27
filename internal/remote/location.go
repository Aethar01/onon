// Package remote opens documents named by URI rather than by local path:
// http(s):// downloads, and files on a host reached over SSH (sftp://,
// ssh://, scp:// or scp's own host:path). Each is fetched into a cache and
// the viewer reads the copy, while the URI stays the document's name for
// sessions, marks and recent files.
//
// SSH paths follow curl, GVfs, KIO and the IETF draft for these schemes:
// sftp://host/dir/file is /dir/file, and sftp://host/~/dir/file is dir/file
// under the remote home. scp-style host:dir/file is relative to the home, as
// in scp and rsync.
package remote

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
)

// sshSchemes are the names accepted for files reached over SSH. All of them
// become sftp://, since that is the protocol spoken.
var sshSchemes = map[string]bool{"sftp": true, "ssh": true, "scp": true}

// davSchemes name WebDAV, as GVfs and KIO do, by the scheme they are
// fetched over. webdav and webdavs become dav and davs.
var davSchemes = map[string]string{"dav": "http", "davs": "https", "webdav": "http", "webdavs": "https"}

// IsURI reports whether s names a remote document by URI.
func IsURI(s string) bool {
	scheme, _, ok := strings.Cut(s, "://")
	if !ok {
		return false
	}
	scheme = strings.ToLower(scheme)
	return scheme == "http" || scheme == "https" || sshSchemes[scheme] || davSchemes[scheme] != ""
}

func schemeOf(uri string) string {
	scheme, _, _ := strings.Cut(uri, "://")
	return strings.ToLower(scheme)
}

// IsDAV reports whether uri names a file on a WebDAV server.
func IsDAV(uri string) bool { return davSchemes[schemeOf(uri)] != "" }

// httpAddress is the http(s) URL a WebDAV URI is fetched from, or uri itself.
func httpAddress(uri string) string {
	scheme, rest, ok := strings.Cut(uri, "://")
	if !ok {
		return uri
	}
	if over := davSchemes[strings.ToLower(scheme)]; over != "" {
		return over + "://" + rest
	}
	return uri
}

// IsSFTP reports whether uri names a file reached over SSH.
func IsSFTP(uri string) bool { return sshSchemes[schemeOf(uri)] }

// Writable reports whether a document at uri can be saved back to where it
// came from: on an SSH host or a WebDAV server, not a plain web server.
func Writable(uri string) bool { return IsSFTP(uri) || IsDAV(uri) }

// Watchable reports whether a document at uri can be checked for changes
// while it is open. A plain web server is not asked over and over.
func Watchable(uri string) bool { return Writable(uri) }

// PageFragment takes a #page=N fragment, as PDF open parameters (RFC 8118)
// give one, off a URI, returning the URI without its fragment and the page,
// or 0 for none. A local path is left whole, since # may be in a file name.
func PageFragment(arg string) (string, int) {
	if !strings.Contains(arg, "://") {
		return arg, 0
	}
	rest, fragment, ok := strings.Cut(arg, "#")
	if !ok {
		return arg, 0
	}
	page := 0
	for _, param := range strings.Split(fragment, "&") {
		if key, value, _ := strings.Cut(param, "="); key == "page" {
			if n, err := strconv.Atoi(value); err == nil && n > 0 {
				page = n
			}
		}
	}
	return rest, page
}

// FromArg turns a document argument into a canonical URI when it names a
// remote document: a URI, or scp's [user@]host:path. A local file of the
// same name wins over host:path, as it does for scp, so a name with a colon
// in it still opens. ok is false for anything else, which is a local path.
func FromArg(arg string) (uri string, ok bool, err error) {
	if IsURI(arg) {
		takeURLLogin(arg)
		uri, err = Canonical(arg)
		return uri, true, err
	}
	if strings.Contains(arg, "://") {
		return "", false, nil // another scheme, such as file://, not host:path
	}
	host, rest, ok := splitSCP(arg)
	if !ok {
		return "", false, nil
	}
	if _, err := os.Lstat(arg); err == nil {
		return "", false, nil
	}
	user := ""
	if at := strings.LastIndex(host, "@"); at >= 0 {
		user, host = host[:at], host[at+1:]
	}
	switch {
	case strings.HasPrefix(rest, "/"):
	case rest == "" || rest == "~":
		rest = "/~"
	case strings.HasPrefix(rest, "~/"):
		rest = "/" + rest
	case strings.HasPrefix(rest, "~"):
		return "", true, fmt.Errorf("%s: ~user paths are not supported", arg)
	default:
		rest = "/~/" + rest
	}
	u := &url.URL{Scheme: "sftp", Host: strings.ToLower(host), Path: rest}
	if user != "" {
		u.User = url.User(user)
	}
	uri, err = Canonical(u.String())
	return uri, true, err
}

// splitSCP splits scp's [user@]host:path. The host comes before the first
// colon, has no slash, and may be an [IPv6] address. A single letter is a
// Windows drive rather than a host.
func splitSCP(arg string) (host, rest string, ok bool) {
	if strings.HasPrefix(arg, "-") {
		return "", "", false
	}
	var colon int
	if at := strings.LastIndex(arg, "@["); at >= 0 || strings.HasPrefix(arg, "[") {
		end := strings.Index(arg, "]:")
		if end < 0 {
			return "", "", false
		}
		colon = end + 1
	} else {
		colon = strings.IndexByte(arg, ':')
	}
	if colon <= 0 {
		return "", "", false
	}
	host = arg[:colon]
	if strings.ContainsAny(host, `/\`) || strings.HasPrefix(host, "@") || strings.HasSuffix(host, "@") {
		return "", "", false
	}
	if len(host) == 1 && isLetter(host[0]) {
		return "", "", false
	}
	return host, arg[colon+1:], true
}

func isLetter(c byte) bool { return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' }

// Canonical returns the one spelling of a remote URI, so that a document
// keeps its session however it was typed: SSH schemes become sftp, the host
// is lowercased, sftp paths lose their doubled leading slash (netrw's
// absolute path), and any ;fingerprint= parameters or password in an sftp
// user are dropped, since ssh itself decides how to authenticate. An
// http(s) URL loses its user and password, which FromArg keeps in memory,
// so that no password is stored with sessions and recent files.
func Canonical(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", Display(raw), err)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Hostname() == "" {
		return "", fmt.Errorf("%s: no host", Display(raw))
	}
	if strings.HasPrefix(u.Hostname(), "-") {
		return "", fmt.Errorf("%s: invalid host", Display(raw))
	}
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	if !sshSchemes[u.Scheme] {
		switch u.Scheme {
		case "webdav":
			u.Scheme = "dav"
		case "webdavs":
			u.Scheme = "davs"
		}
		u.User = nil
		return u.String(), nil
	}
	u.Scheme = "sftp"
	u.RawQuery = ""
	if u.User != nil {
		name, _, _ := strings.Cut(u.User.Username(), ";")
		u.User = nil
		if name != "" {
			u.User = url.User(name)
		}
	}
	if u.Port() == "22" {
		u.Host = u.Hostname()
		if strings.Contains(u.Host, ":") {
			u.Host = "[" + u.Host + "]"
		}
	}
	p := u.Path
	for strings.HasPrefix(p, "//") {
		p = p[1:]
	}
	if p == "" || p == "/" {
		return "", fmt.Errorf("%s: no file path", Display(raw))
	}
	if strings.HasPrefix(p, "/~") && p != "/~" && !strings.HasPrefix(p, "/~/") {
		return "", fmt.Errorf("%s: ~user paths are not supported", Display(raw))
	}
	u.Path = path.Clean(p)
	u.RawPath = ""
	return u.String(), nil
}

// Display returns uri as it should be shown in messages and logs, with any
// password and query hidden, since a signed link carries its token there.
func Display(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return uri
	}
	if u.RawQuery != "" {
		u.RawQuery = "…"
	}
	return u.Redacted()
}

// Name returns the file name in uri, for titles and the cached copy. A URL
// without one, such as a bare site, is named after its host.
func Name(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return path.Base(uri)
	}
	if name := path.Base(u.Path); name != "/" && name != "." && name != "~" {
		return name
	}
	return u.Hostname()
}

// Resolve names the relative path rel beside the remote document base, as
// :open does beside a local document.
func Resolve(base, rel string) (string, error) {
	b, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	return Canonical(b.ResolveReference(&url.URL{Path: rel}).String())
}

// Host returns the host[:port] in uri.
func Host(uri string) string {
	if u, err := url.Parse(uri); err == nil && u.Host != "" {
		return u.Host
	}
	return uri
}

// target is a parsed canonical URI.
type target struct {
	url *url.URL
}

func parseTarget(uri string) (target, error) {
	canonical, err := Canonical(uri)
	if err != nil {
		return target{}, err
	}
	u, err := url.Parse(canonical)
	if err != nil {
		return target{}, err
	}
	return target{url: u}, nil
}

// homeRelative reports whether an sftp path is under the remote home,
// returning the part after ~/.
func (t target) homeRelative() (string, bool) {
	p := t.url.Path
	if p == "/~" {
		return ".", true
	}
	if rest, ok := strings.CutPrefix(p, "/~/"); ok {
		return rest, true
	}
	return "", false
}

// withPath returns the URI of another sftp path on the same host.
func (t target) withPath(p string) string {
	u := *t.url
	u.Path = p
	u.RawPath = ""
	return u.String()
}

// ErrReadOnly is returned when saving a document that cannot be written back.
var ErrReadOnly = errors.New("remote document is read-only")
