package remote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
)

var httpClient = &http.Client{Transport: authTransport{base: http.DefaultTransport}}

// fetchHTTP downloads uri unless the cached copy is still current, which a
// conditional request lets the server say without sending it again. A
// request for a login is answered as httpauth.go describes.
func fetchHTTP(ctx context.Context, uri string, entry cacheEntry, progress func(done, total int64)) (Info, error) {
	cached, hasCache := entry.cached()
	var attempts loginAttempts
	var resp *http.Response
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
		if err != nil {
			return Info{}, err
		}
		req.Header.Set("User-Agent", "onon")
		if hasCache {
			if cached.ETag != "" {
				req.Header.Set("If-None-Match", cached.ETag)
			}
			if cached.LastModified != "" {
				req.Header.Set("If-Modified-Since", cached.LastModified)
			}
		}
		resp, err = httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return Info{}, ctx.Err()
			}
			return Info{}, fmt.Errorf("fetch %s: %w", Display(uri), unwrapURLError(err))
		}
		if resp.StatusCode != http.StatusUnauthorized {
			break
		}
		resp.Body.Close()
		// The site that asks, which a redirect may have reached.
		site := resp.Request.URL
		host := site.Host
		realm, basic := basicRealm(resp.Header.Values("WWW-Authenticate"))
		if !basic || headersFor(host).Get("Authorization") != "" {
			return Info{}, unauthorized(uri, host, resp)
		}
		next, ok := attempts.nextLogin(ctx, site, realm)
		if ctx.Err() != nil {
			return Info{}, ctx.Err()
		}
		if !ok {
			return Info{}, unauthorized(uri, host, resp)
		}
		setLogin(siteKey(site), next)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified && hasCache {
		cached.URI = uri
		return cached, nil
	}
	if resp.StatusCode != http.StatusOK {
		return Info{}, fmt.Errorf("fetch %s: %s", Display(uri), resp.Status)
	}
	info := Info{
		URI:          uri,
		Size:         resp.ContentLength,
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
	}
	if t, err := http.ParseTime(info.LastModified); err == nil {
		info.ModTime = t
	}
	return entry.store(info, httpName(uri, resp), func(w io.Writer) error {
		_, err := io.Copy(&progressWriter{w: w, total: resp.ContentLength, progress: progress}, resp.Body)
		if err != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	})
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
