# onon

A quiet reader for the keyboard, built on MuPDF and configured with Lua.

onon provides Vim-style navigation, continuous and single-page layouts, dual-page spreads, text search and selection, outlines, links, persistent sessions and marks, themes, commands, and scriptable keybindings without a permanent toolbar.

## Quick Start

```bash
onon file.pdf
```

If no file is provided, onon reopens the most recently viewed document.

Useful defaults:

| Key | Action |
|---|---|
| `j` / `k` | Scroll down / up |
| `J` / `K` | Next / previous page |
| `/` / `?` | Search forward / backward |
| `n` / `N` | Next / previous match |
| `o` | Open the document outline |
| `O` | Page overview grid |
| `F5` | Presentation mode |
| `H` | Highlight the selection (pick a colour) |
| `u` / `U` | Undo / redo edits |
| `x` | Delete the annotation under the pointer (right-click for more) |
| `F` | Follow a link by typing its hint |
| `gr` | Open recent files |
| `:` | Open the command prompt |
| `F1` / `g?` | Help: commands, keys, search modifiers and options |
| `q` | Quit |

The usual shortcuts of each system work as well: on macOS Cmd-F to search, Cmd-Z to undo and Cmd-= to zoom, with the menu bar listing the actions and their keys, and on Windows and Linux Ctrl-F, Ctrl-Z, Ctrl-= and F11.

## Installation

Download a package for Linux, macOS, or Windows from the [latest release](https://github.com/Aethar01/onon/releases/latest).

<details open>
<summary>Linux</summary>

The release packages run on Ubuntu 22.04, Fedora 36, Debian 12 and anything newer. They bundle MuPDF and SDL3, so only GTK 3 has to come from the system.

Debian and Ubuntu:

```bash
sudo apt install ./onon-*-linux-x86_64.deb
```

Fedora and openSUSE:

```bash
sudo dnf install ./onon-*-linux-x86_64.rpm    # or: sudo zypper install
```

Any distribution, without installing:

```bash
chmod +x onon-*-linux-x86_64.AppImage
./onon-*-linux-x86_64.AppImage file.pdf
```

Or unpack the tarball into `~/.local`, which puts `onon` in `~/.local/bin`:

```bash
tar -xzf onon-*-linux-x86_64.tar.gz -C ~/.local --strip-components=1
```

Arch-based systems can install [onon-git from the AUR](https://aur.archlinux.org/packages/onon-git):

```bash
yay -S onon-git
```

</details>

<details>
<summary>macOS</summary>

Install the release matching Intel or Apple silicon, or use Homebrew:

```bash
brew install aethar01/tap/onon
```

</details>

<details>
<summary>Windows</summary>

The release provides an installer with optional PDF file association and a portable zip.

</details>

## Usage

```bash
onon /path/to/file.pdf      # open a document
onon --goto 20 file.pdf     # start on page 20
onon --config custom.lua file.pdf
onon --no-config file.pdf   # built-in defaults only
onon --no-plugins file.pdf  # skip plugin loading
onon -v                     # print version
onon -V                     # enable verbose logs
```

Use `F1`, `g?` or `:help` to browse commands, key bindings, search modifiers and options; choosing a row runs it or fills in the prompt. `:keybinds` edits key bindings.

### Remote documents

onon opens documents from a web server or over SSH, on the command line and with `:open`:

```bash
onon https://example.com/paper.pdf
onon sftp://me@host/srv/docs/paper.pdf     # /srv/docs/paper.pdf on host
onon sftp://me@host/~/paper.pdf            # paper.pdf in the home directory
onon me@host:papers/paper.pdf              # scp style, relative to the home
onon davs://cloud.example.com/remote.php/dav/files/me/paper.pdf   # WebDAV
onon 'https://example.com/paper.pdf#page=12'   # open at page 12
```

`ssh://` and `scp://` mean the same as `sftp://`, and a port goes after the host as in `sftp://host:2222/...`. A path after the host is absolute, as in curl and GNOME and KDE file managers. If no file is there, onon looks for it in the home directory, so `ssh://host/paper.pdf` finds `~/paper.pdf`. In scp style, a local file of the same name wins, as it does for scp; write `./host:file` to mean a local one.

`davs://` and `dav://` (or `webdavs://` and `webdav://`) name a file on a WebDAV server, such as Nextcloud, ownCloud or a NAS, fetched over https or http. A `#page=N` at the end of a URI, as in links to papers, opens at that page unless `--goto` says otherwise.

The document is downloaded to the cache directory and opened from there. Sessions, marks and recent files keep the URI, so reopening one returns to where you were and downloads it again only if it has changed. The status bar shows the download, and `Esc` cancels it. `:open name` from a remote document opens `name` beside it on the same host.

SSH goes through the system's `ssh`, so `~/.ssh/config`, host aliases, `ProxyJump`, the agent and `known_hosts` all apply. onon is its own `SSH_ASKPASS`: a password, a key's passphrase or a new host key is asked about in the status bar, whether onon was started from a terminal or not. Esc refuses. This needs OpenSSH 8.4 or later, which current Linux, macOS and Windows 11 have; an older ssh still asks on the terminal when onon was started from one. The host needs an SFTP server, which OpenSSH has by default. One connection per host stays open while onon runs.

A web server that asks for a login, as a browser shows a login dialog for, is answered with a login from the URL (`https://me:pass@host/...`), then one in `~/.netrc` (`machine host login me password ...`, as curl and wget read it), and then by asking for a user name and password in the status bar. A login that works is kept in memory for the rest of the session, by host; none is saved, and a password in a URL is left out of the URI that sessions and recent files keep.

For an API token or any other header, set `http_headers` in `config.lua`, by host, `host:port` or `*.domain`. Taking the value from the environment keeps it out of the file:

```lua
onon.options.http_headers = {
  ["api.example.com"] = { Authorization = "Bearer " .. os.getenv("EXAMPLE_TOKEN") },
  ["*.internal.example"] = { ["X-Api-Key"] = os.getenv("INTERNAL_KEY") },
}
```

The headers go only to the host they are set for, including after a redirect, and `:set` shows their names but not their values. Logins that a web page asks for in a form, such as single sign-on, need a browser; a shared or signed download link works instead. Such a link is kept in recent files as given, since opening it again needs it, but messages and logs leave out its query, where its token is.

`:w` saves edits back to a WebDAV server too, with a `PUT` the server refuses if the file changed since it was opened (by its ETag), so `:w` and `:w!` work as they do over SSH. With `auto_reload`, a WebDAV document is checked for changes every few seconds, using the login already given.

`:w` saves edits back over SSH: the file is written beside the original under a temporary name and renamed over it, keeping its permissions. If the file changed on the host since it was opened, `:w` refuses and `:w!` saves over it anyway. With `auto_reload`, a document on an SSH host is checked for changes every few seconds. Documents from a web server are read-only; `:w ~/paper.pdf` saves a local copy.

Other names for documents can be added in `config.lua` with `onon.register_scheme`. The function is given what follows the scheme and returns the URL or path to open, which may end in `#page=N`:

```lua
onon.register_scheme("arxiv", function(id) return "https://arxiv.org/pdf/" .. id end)
onon.register_scheme("papers", function(name) return "davs://cloud.example.com/dav/papers/" .. name .. ".pdf" end)
```

Then `onon arxiv:2401.12345` or `:open papers:smith2024` opens it. A local file of the same name wins.

### Single instance

Instances are per document. Each window is reachable by anything that opens the
same file.

```bash
onon --unique file.pdf                  # reuse the window showing file.pdf
onon --unique --goto 42 file.pdf        # ... and go to page 42
onon --unique --goto 42:100:250 f.pdf   # ... to a point on page 42
onon --goto 42 file.pdf                 # always a new window, opened at 42
onon --unique --command "reload-config" f.pdf   # run a command in that window
```

`X` and `Y` are points from the page's top-left corner. `--command` takes a
viewer command as typed after `:`, including commands registered by plugins,
and exits non-zero if the command is unrecognised.

With no window showing the document, `--unique` opens one and applies any
`--goto` and `--command` as it starts. A different document is a different
instance, so two files give two windows. The address follows the document: a
window that switches files becomes reachable under the new one.

Sockets live under `$XDG_RUNTIME_DIR/onon` on Linux, the per-user temporary
directory on macOS, and `%LOCALAPPDATA%\onon` on Windows. They are mode 0600
and named by a hash of the document path. A socket left by a crash is reclaimed
on the next start.

## Configuration

Configuration is optional and written in Lua. Start with [`config.example.lua`](./config.example.lua), or create a small file containing only the values and bindings you want to change. `:config`, or Settings… (Cmd-,) on macOS, opens the file in your text editor, creating it if there is none. Reload it with `:reload-config`.

The first existing configuration file for the current platform is loaded:

| Platform | Location |
|---|---|
| Any | Path passed with `--config` |
| Linux | `~/.config/onon/config.lua` |
| Linux | `$XDG_CONFIG_HOME/onon/config.lua` |
| Linux | Each `$XDG_CONFIG_DIRS/onon/config.lua` |
| Linux | `/etc/xdg/onon/config.lua` |
| macOS | `~/Library/Application Support/onon/config.lua` |
| macOS | `~/.config/onon/config.lua` |
| Windows | `%APPDATA%\onon\config.lua` |

Plugins and theme files are looked for in every one of these directories, with `NAME` being `plugins` or `themes`, so those in different directories are all found; where two share a name, the first wins. Theme files beside `config.lua`, in `themes/`, come before all of them.

| Platform | Directory |
|---|---|
| Linux | `/usr/lib/onon/NAME` |
| Linux | `$XDG_DATA_HOME/onon/NAME` or `~/.local/share/onon/NAME` |
| Linux | `$XDG_CONFIG_HOME/onon/NAME` |
| Linux | `~/.config/onon/NAME` |
| Linux | Each `$XDG_CONFIG_DIRS/onon/NAME` |
| macOS | `~/Library/Application Support/onon/NAME` |
| macOS | `~/.config/onon/NAME` |
| Windows | `%LOCALAPPDATA%\onon\NAME` |
| Windows | `%APPDATA%\onon\NAME` |

Example plugins and themes live in [onon-plugins](https://github.com/Aethar01/onon-plugins) and [onon-themes](https://github.com/Aethar01/onon-themes). Clone them into your onon directory and link their `plugins` and `themes` directories beside them:

```bash
cd ~/.config/onon
git clone https://github.com/Aethar01/onon-plugins
git clone https://github.com/Aethar01/onon-themes
ln -s onon-plugins/plugins plugins
ln -s onon-themes/themes themes
```

Themes then appear in `:theme`. Plugins run once `config.lua` requires them; each plugin's README shows how. On Windows, or with `plugins` and `themes` directories of your own, copy in what you want instead.

### Themes

Colours, the UI font and the shape of panels come from one table, `onon.theme`. Fields left out keep the values of the base theme, so a theme can be as small as you like:

```lua
onon.theme = {
  base = "birch",              -- moss (the default), birch or classic
  accent = "#335533",
  alt = { accent = "#88aa88" }, -- colours for alternate-color mode
  font = { family = "Iosevka", size = 14 },
  status_bar_style = "pill",   -- float the status bar over the page
  motion = { scale = 0 },      -- no animation
}
```

The quickest way to change the theme is `:theme`, which lists the built-in themes and the theme files, those in `themes/` beside `config.lua` and in the theme directories above, showing each as you move to it; Enter keeps the one selected and onon remembers it. A theme file returns its table:

```lua
-- themes/forest.lua, next to config.lua
return { base = "moss", accent = "#2f5d3a", alt = { accent = "#8fbf8f" } }
```

Choose it with `:theme forest`, or name it in `config.lua` with `onon.theme = require("themes.forest")`; `require` looks beside `config.lua` first, with dots naming directories. A theme assigned in `config.lua` replaces the one picked with `:theme` each time onon starts, while fields set one at a time apply to whichever is picked.

Tweak a single field with `onon.theme.radius = 0` or, at runtime, `:set theme.radius=0`. With no `font.family` the UI uses the system's interface font. The [reference](https://ononreader.com/) lists every field.

To go further, each piece of the UI, such as a panel, a menu row, the status bar or a link hint, is an element with its own fill, border, shadows, padding and shape. A shape can be any path, written as SVG path data or drawn by a Lua function, and an element's `draw` function can draw its box however it likes:

```lua
onon.theme.elements = {
  panel = { radius = 14, shadow = "0 8 24 shadow" },
  row_selected = { fill = "accent", text = "panel" },
  status_left = { shape = "M0,0 H100%-8 L100%,50% L100%-8,100% H0 Z" },
  hint = {
    draw = function(canvas, box, state)
      canvas:default()
      canvas:stroke("M0,100% H100%", "accent", 2) -- underline each hint
    end,
  },
}
```

Session data, such as each document's place, recent files, prompt history and keybindings changed in the keybind editor, is stored in `session.sqlite` under the platform application-data directory. Keybindings changed in the editor apply before `config.lua`, so explicit user configuration takes precedence. Older versions kept them in `autogen.lua`; onon moves one it finds into the database and renames it `autogen.lua.bak`.

| Platform | Location |
|---|---|
| Linux | `$XDG_DATA_HOME/onon` or `~/.local/share/onon` |
| macOS | `~/Library/Application Support/onon` |
| Windows | `%APPDATA%\onon` |

## Documentation

The [documentation site](https://ononreader.com/) covers:

- Configuration options and defaults
- Commands and search modifiers
- Lua functions and tables
- Bindable actions and default keys

The site provides documentation for the current `git` branch and an immutable snapshot for each tagged release. Reference content and the example configuration are generated from the same registrations used by the application:

```bash
go generate ./...
```

## Building

Requirements:

- Go 1.25+
- MuPDF 1.25.6+
- SDL3
- pkg-config/pkgconf
- A C compiler supported by CGO

```bash
go build
go test ./...
```

On Windows, install the dependencies from MSYS2 UCRT64:

```bash
pacman -S --needed mingw-w64-ucrt-x86_64-go mingw-w64-ucrt-x86_64-gcc mingw-w64-ucrt-x86_64-pkgconf mingw-w64-ucrt-x86_64-sdl3 mingw-w64-ucrt-x86_64-mupdf
go build -o onon.exe
```

## The name

onon (said oh-non) is named after Onon, an island in the West Reach of Ursula K. Le Guin's Earthsea, as it appears on her map of the archipelago.

## License

onon is licensed under the [AGPL](./LICENSE).

It links against [MuPDF](https://mupdf.com/), which is licensed under the AGPL unless you have a separate commercial license.
