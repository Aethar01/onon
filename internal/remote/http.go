package remote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"
)

var httpClient = &http.Client{
	Transport: authTransport{base: http.DefaultTransport},
	// Only a GET or HEAD follows a redirect: net/http sends a redirected PUT
	// again as a GET, whose 200 would pass for a save.
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if method := via[0].Method; method != http.MethodGet && method != http.MethodHead {
			return http.ErrUseLastResponse
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	},
}

// doHTTP sends the request newRequest makes, answering a request for a
// login as httpauth.go describes and sending the request again, which is
// why it is made afresh each time. Without ask, a login is never asked for,
// for checks made in the background. uri names the document in errors.
func doHTTP(ctx context.Context, uri string, ask bool, newRequest func() (*http.Request, error)) (*http.Response, error) {
	var attempts loginAttempts
	if !ask {
		attempts.netrcTried, attempts.asked = true, maxLoginPrompts
	}
	for {
		req, err := newRequest()
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "onon")
		resp, err := httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("%s: %w", Display(uri), unwrapURLError(err))
		}
		if resp.StatusCode != http.StatusUnauthorized {
			return resp, nil
		}
		resp.Body.Close()
		// The site that asks, which a redirect may have reached.
		site := resp.Request.URL
		host := site.Host
		realm, basic := basicRealm(resp.Header.Values("WWW-Authenticate"))
		if !basic || headersFor(host).Get("Authorization") != "" {
			return nil, unauthorized(uri, host, resp)
		}
		if !ask {
			return nil, unauthorized(uri, host, resp)
		}
		next, ok := attempts.nextLogin(ctx, site, realm)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !ok {
			return nil, unauthorized(uri, host, resp)
		}
		setLogin(siteKey(site), next)
	}
}

// fetchHTTP downloads the document uri from address, its http(s) URL,
// unless the cached copy is still current, which a conditional request lets
// the server say without sending it again.
func fetchHTTP(ctx context.Context, uri, address string, entry cacheEntry, progress func(done, total int64)) (Info, error) {
	cached, hasCache := entry.cached()
	resp, err := doHTTP(ctx, uri, true, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
		if err != nil {
			return nil, err
		}
		// The bytes as stored, with the ETag a HEAD gives, which a compressed
		// reply may not have.
		req.Header.Set("Accept-Encoding", "identity")
		if hasCache {
			if cached.ETag != "" {
				req.Header.Set("If-None-Match", cached.ETag)
			}
			if cached.LastModified != "" {
				req.Header.Set("If-Modified-Since", cached.LastModified)
			}
		}
		return req, nil
	})
	if err != nil {
		if ctx.Err() != nil {
			return Info{}, ctx.Err()
		}
		return Info{}, fmt.Errorf("fetch %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified && hasCache {
		cached.URI = uri
		return cached, nil
	}
	if resp.StatusCode != http.StatusOK {
		return Info{}, fmt.Errorf("fetch %s: %s", Display(uri), resp.Status)
	}
	info := versionOf(resp)
	info.URI = uri
	return entry.store(info, httpName(uri, resp), func(w io.Writer) error {
		_, err := io.Copy(&progressWriter{w: w, total: resp.ContentLength, progress: progress}, resp.Body)
		if err != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	})
}

// versionOf is the version of a document a response describes.
func versionOf(resp *http.Response) Info {
	info := Info{
		Size:         resp.ContentLength,
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
	}
	if t, err := http.ParseTime(info.LastModified); err == nil {
		info.ModTime = t
	}
	return info
}

// statHTTP asks for the current version of the document at uri with a HEAD
// request, never asking for a login, for checks made in the background.
func statHTTP(uri string) (Info, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	resp, err := doHTTP(ctx, uri, false, func() (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodHead, httpAddress(uri), nil)
	})
	if err != nil {
		return Info{}, false, err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Info{}, false, fmt.Errorf("%s: %s", Display(uri), resp.Status)
	}
	info := versionOf(resp)
	info.URI = uri
	return info, true, nil
}

// uploadDAV saves local over the document at uri with a WebDAV PUT. Unless
// force is set the server is asked to refuse if the file is no longer the
// version opened, by its ETag, or failing that its modification time.
func uploadDAV(ctx context.Context, uri, local string, opened Info, force bool) (Info, error) {
	st, err := os.Stat(local)
	if err != nil {
		return Info{}, err
	}
	address := httpAddress(uri)
	resp, err := doHTTP(ctx, uri, true, func() (*http.Request, error) {
		f, err := os.Open(local)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, address, f)
		if err != nil {
			f.Close()
			return nil, err
		}
		req.ContentLength = st.Size()
		if !force {
			// A weak ETag never matches If-Match, which compares strongly.
			if opened.ETag != "" && !strings.HasPrefix(opened.ETag, "W/") {
				req.Header.Set("If-Match", opened.ETag)
			} else if opened.LastModified != "" {
				req.Header.Set("If-Unmodified-Since", opened.LastModified)
			}
		}
		return req, nil
	})
	if err != nil {
		return Info{}, err
	}
	resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusNoContent:
	case http.StatusPreconditionFailed:
		return Info{}, ErrConflict
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return Info{}, fmt.Errorf("%s: %s to %s; open it there to save it", Display(uri), resp.Status, resp.Header.Get("Location"))
	case http.StatusMethodNotAllowed, http.StatusForbidden:
		return Info{}, fmt.Errorf("%s: %s; the server does not accept saving it", Display(uri), resp.Status)
	default:
		return Info{}, fmt.Errorf("%s: %s", Display(uri), resp.Status)
	}
	info := versionOf(resp)
	if info.ETag == "" {
		// Not every server says what it stored; ask.
		if current, ok, err := statHTTP(uri); err == nil && ok {
			info = current
		}
	}
	info.URI, info.Local, info.Size = uri, local, st.Size()
	return info, nil
}

// httpName names a download: by the URL's last path element when it has an
// extension, else by Content-Disposition, else after its Content-Type.
func httpName(uri string, resp *http.Response) string {
	name := Name(uri)
	if path.Ext(name) != "" {
		return name
	}
	if _, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); err == nil && params["filename"] != "" {
		return path.Base(params["filename"])
	}
	if mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err == nil {
		if ext := knownExtension(mediaType); ext != "" {
			return name + ext
		}
	}
	return name
}

// knownExtension maps the document types servers commonly send to the
// extension MuPDF expects; mime's own table differs between systems.
func knownExtension(mediaType string) string {
	switch mediaType {
	case "application/pdf":
		return ".pdf"
	case "application/epub+zip":
		return ".epub"
	case "application/oxps", "application/vnd.ms-xpsdocument":
		return ".xps"
	case "application/x-cbz", "application/vnd.comicbook+zip":
		return ".cbz"
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/svg+xml":
		return ".svg"
	case "text/html":
		return ".html"
	case "text/plain":
		return ".txt"
	}
	if exts, err := mime.ExtensionsByType(mediaType); err == nil && len(exts) > 0 {
		return exts[0]
	}
	return ""
}

// unwrapURLError drops the method and URL that *url.Error adds, since the
// message already names the document, and the URL may hold a password.
func unwrapURLError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}
