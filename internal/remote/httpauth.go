package remote

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// A web server that wants a login answers 401 with a Basic challenge, which
// a browser shows as its login dialog. onon answers with, in turn, a login
// already accepted this session, one from the URL, one from ~/.netrc, and
// then by asking in the window, up to three times. Logins are kept in memory
// only, by host, and never in the URIs that sessions and recent files keep.
//
// Headers set for a host in the http_headers option, such as an API token,
// go with every request to that host. They and the logins are added per
// request, so a redirect to another host never carries them there.

type login struct {
	user, pass string
}

var (
	authMu sync.Mutex
	logins = map[string]login{}  // by scheme://host[:port]
	users  = map[string]string{} // user names from URLs without a password, to offer, by host
	extra  = map[string]http.Header{}
)

// SetHTTPHeaders sets the headers sent with requests to each host. A key is
// a host, a host:port, or *.domain for the domain's subdomains.
func SetHTTPHeaders(byHost map[string]map[string]string) {
	headers := map[string]http.Header{}
	for host, values := range byHost {
		h := http.Header{}
		for name, value := range values {
			h.Set(name, value)
		}
		headers[strings.ToLower(host)] = h
	}
	authMu.Lock()
	extra = headers
	authMu.Unlock()
}

// headersFor returns the configured headers for host, most specific first:
// host:port, then host, then the closest *.domain.
func headersFor(host string) http.Header {
	authMu.Lock()
	defer authMu.Unlock()
	host = strings.ToLower(host)
	if h, ok := extra[host]; ok {
		return h
	}
	name := hostname(host)
	if h, ok := extra[name]; ok {
		return h
	}
	for domain := name; ; {
		_, rest, ok := strings.Cut(domain, ".")
		if !ok {
			return nil
		}
		if h, ok := extra["*."+rest]; ok {
			return h
		}
		domain = rest
	}
}

func hostname(host string) string {
	if u, err := url.Parse("//" + host); err == nil {
		return u.Hostname()
	}
	return host
}

// A login is kept for a scheme and host together, so one given over https
// is never sent in the clear to the same host over http.
func siteKey(u *url.URL) string {
	return strings.ToLower(u.Scheme + "://" + u.Host)
}

func loginFor(site string) (login, bool) {
	authMu.Lock()
	defer authMu.Unlock()
	l, ok := logins[site]
	return l, ok
}

func setLogin(site string, l login) {
	authMu.Lock()
	logins[site] = l
	authMu.Unlock()
}

func forgetLogin(site string) {
	authMu.Lock()
	delete(logins, site)
	authMu.Unlock()
}

// takeURLLogin keeps the user and password in an http(s) URL for its host,
// since the URL itself is stored without them.
func takeURLLogin(raw string) {
	u, err := url.Parse(httpAddress(raw))
	if err != nil || u.User == nil || (u.Scheme != "http" && u.Scheme != "https") {
		return
	}
	host := strings.ToLower(u.Host)
	if pass, ok := u.User.Password(); ok {
		setLogin(siteKey(u), login{user: u.User.Username(), pass: pass})
		return
	}
	authMu.Lock()
	users[host] = u.User.Username()
	authMu.Unlock()
}

// authTransport adds each request's host's headers and login, except over
// a redirect from https down to http, which would send them in the clear.
type authTransport struct {
	base http.RoundTripper
}

func (t authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme == "http" && req.Response != nil && req.Response.Request.URL.Scheme == "https" {
		return t.base.RoundTrip(req)
	}
	headers := headersFor(req.URL.Host)
	l, hasLogin := loginFor(siteKey(req.URL))
	if len(headers) == 0 && !hasLogin {
		return t.base.RoundTrip(req)
	}
	req = req.Clone(req.Context())
	if hasLogin {
		req.SetBasicAuth(l.user, l.pass)
	}
	for name, values := range headers {
		req.Header[name] = values // configured headers win, Authorization included
	}
	return t.base.RoundTrip(req)
}

// basicRealm finds a Basic challenge among a 401's WWW-Authenticate headers
// and returns its realm.
func basicRealm(challenges []string) (realm string, ok bool) {
	for _, challenge := range challenges {
		scheme, params, _ := strings.Cut(strings.TrimSpace(challenge), " ")
		if !strings.EqualFold(scheme, "Basic") {
			continue
		}
		for _, param := range strings.Split(params, ",") {
			key, value, _ := strings.Cut(strings.TrimSpace(param), "=")
			if strings.EqualFold(key, "realm") {
				return strings.Trim(value, `"`), true
			}
		}
		return "", true
	}
	return "", false
}

// loginAttempts tracks how a 401 is answered during one fetch.
type loginAttempts struct {
	netrcTried bool
	asked      int
}

const maxLoginPrompts = 3

// nextLogin finds a login to try after host refused the last one, or
// reports false when there is nothing left to try.
func (a *loginAttempts) nextLogin(ctx context.Context, site *url.URL, realm string) (login, bool) {
	host := site.Host
	failed, hadLogin := loginFor(siteKey(site))
	forgetLogin(siteKey(site))
	if !a.netrcTried {
		a.netrcTried = true
		if user, pass, ok := netrcLogin(hostname(host)); ok && (!hadLogin || failed != (login{user, pass})) {
			return login{user: user, pass: pass}, true
		}
	}
	p := currentPrompter()
	if p == nil || a.asked >= maxLoginPrompts {
		return login{}, false
	}
	a.asked++
	name := host
	if realm != "" {
		name += " (" + realm + ")"
	}
	question := "Username for " + name
	if a.asked > 1 || hadLogin {
		question = "Login refused; username for " + name
	}
	authMu.Lock()
	user := users[strings.ToLower(host)]
	authMu.Unlock()
	if hadLogin {
		user = failed.user
	}
	user, ok := p(ctx, Prompt{Host: host, Text: question, Kind: PromptText, Initial: user})
	if !ok {
		return login{}, false
	}
	pass, ok := p(ctx, Prompt{Host: host, Text: "Password for " + user + "@" + host, Kind: PromptSecret})
	if !ok {
		return login{}, false
	}
	return login{user: user, pass: pass}, true
}

// unauthorized explains a 401 that could not be answered.
func unauthorized(uri, host string, resp *http.Response) error {
	if headersFor(host).Get("Authorization") != "" {
		return fmt.Errorf("%s: %s with the Authorization header set for %s in http_headers", Display(uri), resp.Status, host)
	}
	if _, basic := basicRealm(resp.Header.Values("WWW-Authenticate")); basic {
		return fmt.Errorf("%s: %s", Display(uri), resp.Status)
	}
	scheme := "a login"
	if challenge := resp.Header.Get("WWW-Authenticate"); challenge != "" {
		scheme, _, _ = strings.Cut(challenge, " ")
	}
	return fmt.Errorf("%s: %s; the server wants %s, which needs a header in http_headers for %s", Display(uri), resp.Status, scheme, host)
}

// NetrcPath is the netrc file read for logins: $NETRC, or .netrc (_netrc
// on Windows, as curl also accepts) in the home directory.
var NetrcPath = func() string {
	if path := os.Getenv("NETRC"); path != "" {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	path := filepath.Join(home, ".netrc")
	if _, err := os.Stat(path); err != nil && runtime.GOOS == "windows" {
		return filepath.Join(home, "_netrc")
	}
	return path
}

// netrcLogin looks host up in the netrc file, falling back to its default
// entry, as curl and wget do.
func netrcLogin(host string) (user, pass string, ok bool) {
	path := NetrcPath()
	if path == "" {
		return "", "", false
	}
	f, err := os.Open(path)
	if err != nil {
		return "", "", false
	}
	defer f.Close()

	type entry struct {
		machine, login, password string
		isDefault                bool
	}
	var entries []entry
	var current *entry
	inMacro := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if inMacro {
			inMacro = strings.TrimSpace(line) != "" // a macro ends at a blank line
			continue
		}
		fields := strings.Fields(line)
		for i := 0; i < len(fields); i++ {
			value := func() string {
				if i+1 < len(fields) {
					i++
					return fields[i]
				}
				return ""
			}
			switch fields[i] {
			case "machine":
				entries = append(entries, entry{machine: strings.ToLower(value())})
				current = &entries[len(entries)-1]
			case "default":
				entries = append(entries, entry{isDefault: true})
				current = &entries[len(entries)-1]
			case "login":
				if current != nil {
					current.login = value()
				}
			case "password":
				if current != nil {
					current.password = value()
				}
			case "account":
				value()
			case "macdef":
				value()
				inMacro = true
				i = len(fields)
			}
		}
	}
	host = strings.ToLower(host)
	for _, e := range entries {
		if e.machine == host && !e.isDefault {
			return e.login, e.password, e.login != "" || e.password != ""
		}
	}
	for _, e := range entries {
		if e.isDefault {
			return e.login, e.password, e.login != "" || e.password != ""
		}
	}
	return "", "", false
}
