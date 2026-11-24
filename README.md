<div align="center">

<img src="./assets/onon.svg" alt="onon logo" width="84" />

# onon

**A quiet reader for the keyboard.**

Built on [MuPDF](https://mupdf.com/) and SDL3, configured in Lua, for macOS, Linux and Windows.

[Website](https://ononreader.com/) · [Documentation](https://ononreader.com/docs/) · [Releases](https://github.com/Aethar01/onon/releases/latest) · [Themes](https://github.com/Aethar01/onon-themes) · [Plugins](https://github.com/Aethar01/onon-plugins)

</div>

<br />

<p align="center">
  <img src="https://ononreader.com/screenshots/hero.webp" alt="Animated demo of onon: dual-page spreads, tabs, live outline preview, dark mode, link hints and page overview in the default moss theme" width="100%" />
</p>

## What is onon?

Most document viewers surround the page with toolbars, sidebars and ribbons, and expect a pointer for every step. **onon** *(said oh-non)* stays out of the way: pages on a calm canvas, a single status bar you can hide, and keyboard controls shaped around how Vim moves through text.

It opens at the exact page, scroll position and zoom where you left off. Navigation, search, outlines, link following, annotations, tabs and settings are all reachable from a few keys — while the usual macOS, Linux and Windows shortcuts, menu bar, drag-to-select, trackpad pinch-zoom and printing still work whenever your hand is already on the mouse.

Under the hood, onon pairs **MuPDF** for fast, sharp rendering (PDF, EPUB, XPS, MOBI, FictionBook, CBZ/CBT comic archives and images) with a **Lua** runtime that lets you configure anything from a single option or accent colour to custom keybindings, URI schemes, GLSL shaders and full plugins with their own panels.

## At a glance

<table>
  <tr>
    <td width="50%" align="center">
      <img src="https://ononreader.com/screenshots/outline.png" alt="Document outline and search highlights on the EHT M87* paper in moss theme" width="100%" />
      <br />
      <sub><strong>Outline &amp; search</strong> — filterable table of contents (<code>o</code>) with live background previews and Vim-style search (<code>/</code>).</sub>
    </td>
    <td width="50%" align="center">
      <img src="https://ononreader.com/screenshots/hints.png" alt="Link hints and PDF highlights on the LIGO GW150914 paper in moss theme" width="100%" />
      <br />
      <sub><strong>Link hints &amp; highlights</strong> — follow any link from the keyboard (<code>F</code>) and save highlights (<code>H</code>) straight into the PDF.</sub>
    </td>
  </tr>
  <tr>
    <td width="50%" align="center">
      <img src="https://ononreader.com/screenshots/overview.png" alt="Page overview thumbnail grid of Oliver Byrne's Euclid in moss theme" width="100%" />
      <br />
      <sub><strong>Page overview</strong> — browse every page as a grid (<code>O</code>) and jump straight to any leaf.</sub>
    </td>
    <td width="50%" align="center">
      <img src="https://ononreader.com/screenshots/dark.png" alt="Alternate dark colour mode on the EHT M87* black hole paper keeping images in full colour" width="100%" />
      <br />
      <sub><strong>Alternate colours</strong> — switch to dark moss (<code>Ctrl-R</code>) with recoloured pages that keep raster images in their own colours.</sub>
    </td>
  </tr>
</table>

<p align="center">
  <sub>
    Screenshots use default settings with the built-in <code>moss</code> theme. Documents shown:
    <a href="https://github.com/jemmybutton/byrne-euclid">Oliver Byrne's <em>Elements of Euclid</em></a> (CC BY-SA 4.0),
    <a href="https://arxiv.org/abs/1906.11238">EHT Collaboration (2019)</a> (CC BY 3.0), and
    <a href="https://arxiv.org/abs/1602.03837">LIGO Scientific &amp; Virgo Collaborations (2016)</a> (CC BY 4.0).
  </sub>
</p>

## Features

- **Keyboard-first reading** — Vim motions (`j`/`k` scroll, `J`/`K` pages, `gg`/`G`, numeric counts), a persistent jump list (`Ctrl-O`/`Ctrl-I`), per-document marks (`"` and `'`), continuous or single-page layouts, dual-page spreads (`d`), margin trimming, hover link previews, home-row link hints (`F`), and presentation mode (`F5`).
- **Vim-style search & outlines** — Search forward (`/`) and backward (`?`) with inline modifiers for case (`\C`), whole words (`\<word\>`), Go regular expressions (`\v`), and current-page scope (`\%.p`). Browse all matches (`gm`) or filter the folding document outline (`o`) while previewing destinations live.
- **Annotations, forms & sessions** — Select text, highlight in customisable colours (`H`), edit or delete annotations (`x` or right-click), undo/redo (`u`/`U`), fill PDF forms, and write changes directly back into the PDF (`:w`). Every document's place, zoom, marks, recent history and open tabs persist in SQLite.
- **Tabs & editor workflows** — Open multiple documents in tabs along the title row, top, bottom or a resizable sidebar (or use native macOS window tabs with `native_tabs = true`). Documents reload automatically when LaTeX, Typst or another tool rebuilds them, and `--unique --goto PAGE:X:Y` lets editors such as Neovim and VS Code drive an open window directly.
- **Remote documents** — Open files over HTTP/HTTPS, SSH/SFTP (`me@host:paper.pdf`) and WebDAV (`davs://`), complete with in-app credential prompts, caching, auto-reload, `#page=N` fragments, and `:w` saving edits back over SSH or WebDAV. Register shorthand schemes of your own in Lua (`onon arxiv:2401.12345`).
- **Lua configuration, themes & plugins** — Start with zero configuration, or open `config.lua` with `:config` and reload live with `:reload-config`. Choose built-in themes (`moss`, `birch`, `classic`) via `:theme`, restyle any UI element down to SVG paths and experimental GLSL shaders, or add plugins and custom interactive panels with `onon.ui.create`.

## Installation

Download a release for macOS, Linux or Windows from the [latest release](https://github.com/Aethar01/onon/releases/latest), or install via a package manager:

<details open>
<summary><strong>macOS</strong> (macOS 12+)</summary>

Install with [Homebrew](https://brew.sh/):

```bash
brew install aethar01/tap/onon
```

Or download the `.dmg` for Apple silicon or Intel from the [latest release](https://github.com/Aethar01/onon/releases/latest) and drag `onon.app` into Applications.

</details>

<details>
<summary><strong>Linux</strong></summary>

Release packages run on Ubuntu 22.04, Fedora 36, Debian 12 and newer. They bundle MuPDF and SDL3, so only GTK 3 comes from the system.

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
./onon-*-linux-x86_64.AppImage paper.pdf
```

Or unpack the tarball into `~/.local` (places `onon` in `~/.local/bin`):

```bash
tar -xzf onon-*-linux-x86_64.tar.gz -C ~/.local --strip-components=1
```

Arch-based systems can install [`onon-git`](https://aur.archlinux.org/packages/onon-git) from the AUR:

```bash
yay -S onon-git
```

</details>

<details>
<summary><strong>Windows</strong></summary>

Download the installer (with optional PDF file association) or the portable `.zip` from the [latest release](https://github.com/Aethar01/onon/releases/latest).

</details>

## Quick start

Open a document from your file manager, drop it onto the window, or launch from the terminal:

```bash
onon paper.pdf                              # open a document (no args reopens the last session)
onon --goto 20 paper.pdf                    # start on page 20
onon --unique --goto 42:100:250 paper.pdf   # reuse the window showing paper.pdf (SyncTeX / editors)
onon --tab notes.pdf                        # open in a new tab of the last focused window
onon https://example.com/paper.pdf#page=12  # open from the web at page 12
onon me@host:papers/paper.pdf               # open over SSH (and :w saves edits back)
```

### Essential keys

Counts work as in Vim (`10j`, `3J`). System shortcuts (`Cmd`/`Ctrl` + `F`, `Z`, `=`, `T`, `W`, `S`) work alongside these:

| Key | Action |
|---|---|
| `j` / `k` | Scroll down / up |
| `J` / `K` | Next / previous page |
| `gg` / `G` | First / last page |
| `d` / `m` | Toggle dual-page spreads / single-page mode |
| `/` / `?` | Search forward / backward (`n` / `N` for next / previous match) |
| `o` / `O` | Document outline / page overview grid |
| `F` | Follow a link by typing its hint |
| `Ctrl-O` / `Ctrl-I` | Jump back / forward in jump history |
| `Ctrl-R` | Toggle alternate (dark) colours |
| `H` / `x` / `u` / `U` | Highlight selection / delete annotation / undo / redo |
| `gt` / `gT` | Next / previous tab (`Ctrl-T` / `Cmd-T` for new tab) |
| `gr` | Recent files |
| `F5` | Presentation mode |
| `:` | Command prompt (`:w`, `:theme`, `:options`, `:keybinds`, `:open`, …) |
| `F1` / `g?` | Interactive help: every command, key, search modifier and option |
| `q` | Close tab or quit (`:qa` quits all tabs) |

## Configuration, themes & plugins

onon needs no configuration. When you want to change defaults, run `:config` (or `Cmd-,` on macOS) to open `config.lua` in your editor, and `:reload-config` to apply edits without restarting. Only include what you want to override:

```lua
-- Options (onon.options or onon.o)
onon.o.dual_page = true
onon.o.fit_mode = "page"
onon.o.tab_bar_position = "title" -- "title", "top", "bottom", "left", or "right"

-- Themes: tweak fields on top of whichever theme is picked with :theme
onon.theme.font = { family = "Iosevka", size = 14 }
onon.theme.status_bar_style = "pill"

-- Bind keys to built-in actions or Lua functions
onon.bind("<C-d>", onon.next_page)
onon.bind("r", function()
  onon.rotation(onon.rotation() + 90)
end)

-- Register custom document schemes (e.g. `onon arxiv:2401.12345`)
onon.register_scheme("arxiv", function(id)
  return "https://arxiv.org/pdf/" .. id
end)
```

Every available option and default binding is listed in [`config.example.lua`](./config.example.lua) and can be browsed or edited live inside onon with `:options` and `:keybinds`.

### Themes and plugins

- **Themes**: Three built-in themes (`moss`, `birch`, `classic`) can be previewed live with `:theme`. Seven more — `brutalist`, `gameboy`, `glass`, `manuscript`, `phosphor`, `synthwave` and `vampire`, each with optional GLSL shaders — live in [onon-themes](https://github.com/Aethar01/onon-themes).
- **Plugins**: Add new commands, actions, events, background jobs and custom UI panels. Example plugins (`file-browser` and `synctex`) live in [onon-plugins](https://github.com/Aethar01/onon-plugins).

On Linux and macOS, clone either repository into your config directory and link it in:

```bash
cd ~/.config/onon
git clone https://github.com/Aethar01/onon-themes
git clone https://github.com/Aethar01/onon-plugins
ln -s onon-themes/themes themes
ln -s onon-plugins/plugins plugins
```

Themes appear in `:theme` immediately; plugins load once your `config.lua` requires them. See the [themes guide](https://ononreader.com/docs/themes/) and [plugins guide](https://ononreader.com/docs/plugins/) for platform paths and Windows instructions.

## Documentation

Full guides and generated reference documentation live at **[ononreader.com/docs](https://ononreader.com/docs/)**:

- [Getting started](https://ononreader.com/docs/) — installation and first steps
- [Reading](https://ononreader.com/docs/reading/) — navigation, search modifiers, links, marks, highlights and presentation mode
- [Remote documents](https://ononreader.com/docs/remote/) — HTTP/HTTPS, SSH/SFTP, WebDAV, credentials, headers and custom schemes
- [Configuration](https://ononreader.com/docs/configuration/) — config paths, options, tabs, status bar templates and keybindings
- [Themes](https://ononreader.com/docs/themes/) — palettes, fonts, element styling, SVG shapes and shaders
- [Scripting with Lua](https://ononreader.com/docs/scripting/) — custom functions, document inspection and `onon.ui.create` panels & forms
- [Plugins](https://ononreader.com/docs/plugins/) — installing and writing plugins
- [Command line and editors](https://ononreader.com/docs/command-line/) — flags, single-instance sockets and LaTeX / SyncTeX integration
- [Full reference](https://ononreader.com/docs/reference/) — every option, command, action and Lua API, generated from source via `go generate ./...`

## Building from source

<details>
<summary>Requirements and platform build commands</summary>

### Requirements

- Go 1.25+
- MuPDF 1.25.6+
- SDL3 3.4+
- `glslang` (compiles theme shaders)
- `SPIRV-Cross` on macOS and Windows (translates shaders for Metal and Direct3D; Linux does not need it)
- On Windows, `dxcompiler.dll` from Microsoft's [DirectX Shader Compiler](https://github.com/microsoft/DirectXShaderCompiler/releases) beside `onon.exe` to run shaders on Direct3D 12 (without it, elements render without shaders)
- `pkg-config` / `pkgconf` and a C/C++ compiler supported by CGO

### Build and test

```bash
go build
go test ./...
```

Reference documentation and [`config.example.lua`](./config.example.lua) are generated from the same registries the application uses:

```bash
go generate ./...
```

### macOS (Homebrew)

```bash
brew install go sdl3 mupdf glslang spirv-cross
export CGO_CFLAGS="-I$(brew --prefix)/include"
export CGO_LDFLAGS="-L$(brew --prefix)/lib"
export DYLD_LIBRARY_PATH="$(brew --prefix)/lib:$DYLD_LIBRARY_PATH"
go build
```

### Windows (MSYS2 UCRT64)

```bash
pacman -S --needed mingw-w64-ucrt-x86_64-go mingw-w64-ucrt-x86_64-gcc mingw-w64-ucrt-x86_64-pkgconf mingw-w64-ucrt-x86_64-sdl3 mingw-w64-ucrt-x86_64-mupdf mingw-w64-ucrt-x86_64-glslang mingw-w64-ucrt-x86_64-spirv-cross
go build -o onon.exe
```

### Packaging

Packagers can embed the version and package manager name so `:check-updates` suggests upgrading with the package manager instead of downloading a release:

```bash
go build -ldflags "-X onon/internal/config.Version=1.2.3 -X onon/internal/config.InstallMethod=aur"
```

Versions matching `git describe --long --tags` (e.g. `0.9.4-3-g49e499b`) or AUR `-git` format (`0.9.4.r3.g49e499b`) check for newer commits on `main` rather than tagged releases.

</details>

## The name

onon *(said oh-non)* is named after Onon, an island in the West Reach of Ursula K. Le Guin's *Earthsea*, as it appears on her map of the archipelago.

## License

onon is licensed under the [GNU AGPLv3](./LICENSE). It links against [MuPDF](https://mupdf.com/), which is licensed under the AGPL unless you hold a separate commercial licence.
