package config

import (
	"net/url"
	"path/filepath"
	"strings"

	"onon/internal/remote"
)

func DataDir() string {
	return platformDataDir()
}

func PluginPaths() []string {
	return unique(platformSearchPaths("plugins"))
}

// ThemePaths are the directories :theme looks in after the one beside
// config.lua, the same as PluginPaths with themes in place of plugins.
func ThemePaths() []string {
	return unique(platformSearchPaths("themes"))
}

// AbsoluteDocumentPath makes a document path absolute. A remote URI is left
// as it is, and a file:// URI, which desktop launchers pass, becomes the path
// it names.
func AbsoluteDocumentPath(path string) string {
	if remote.IsURI(path) {
		return path
	}
	if local, ok := fileURIPath(path); ok {
		path = local
	}
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

func fileURIPath(uri string) (string, bool) {
	if len(uri) < len("file://") || !strings.EqualFold(uri[:len("file://")], "file://") {
		return "", false
	}
	u, err := url.Parse(uri)
	if err != nil || (u.Host != "" && !strings.EqualFold(u.Host, "localhost")) || u.Path == "" {
		return "", false
	}
	path := u.Path
	// file:///C:/dir/file on Windows
	if len(path) > 2 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	return filepath.FromSlash(path), true
}

// DocumentName is the name a document is shown by: its file name, or the
// file name in a remote URI.
func DocumentName(path string) string {
	if path == "" {
		return ""
	}
	if remote.IsURI(path) {
		return remote.Name(path)
	}
	return filepath.Base(path)
}
