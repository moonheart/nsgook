<p align="center">
  <img src="assets/Icon.png" alt="NSGook Icon" width="96" />
</p>

<h1 align="center">NSGook</h1>

<p align="center">
  A pure-Go <a href="https://github.com/QL-Win/QuickLook">QuickLook</a> rewrite for Windows powered by <a href="https://github.com/nsgo-dev/nsgo">NSGo</a>.
</p>

<p align="center">
  English | <a href="README_zh.md">简体中文</a>
</p>

---

## Features

- **Instant File Preview on Space**: Tap `Space` on a selected file to toggle the preview window, or hold `Space` (`>= 750ms`) to peek and release to close.
- **Native Windows Shell Integration**: Low-level keyboard & foreground hooks with selection detection for Windows File Explorer, Desktop, Everything, and standard Open/Save file dialogs (`#32770`), plus CLI arguments (`--top`, `--pin`) and drag-and-drop.
- **Multi-Window Pinning & Window Chrome**: Pin previews to keep multiple windows side by side, toggle Always on Top, use 9-zone auto-sizing and translucent backdrops (Acrylic / Mica / Tabbed), and switch between Light, Dark, and System themes.
- **Bilingual UI & Searchable Settings**: Built-in English (`en`) and Simplified Chinese (`zh-Hans`) localizations with system tray integration and YAML configuration (`extension_allowlist` / `extension_blocklist`, viewer preferences, etc.).
- **Bounded Memory Usage**: Reuses hidden preview windows to avoid desktop graphics handle churn while explicitly flushing decoded images and media views before unmount.

## Keyboard Shortcuts

| Key | Action |
| --- | --- |
| `Space` (tap) | Toggle preview open / closed |
| `Space` (hold & release) | Hold-to-preview auto-close (configurable via `auto_close_holding`) |
| `Up` / `Down` / `Left` / `Right` | Switch to newly selected file while preview is open |
| `Enter` | Open file with default application and close preview |
| `Esc` | Close active preview |
| `F5` | Reload current preview |
| `F11` | Toggle fullscreen |

## Built-in Viewer Plugins (26)

### Core Viewers

| Plugin | ID | Capabilities |
| --- | --- | --- |
| ImageViewer | `imageviewer` | Raster images (`PNG`, `JPEG`, `GIF`, `WebP`, `BMP`, `TIFF`, `QOI`, `ICO`, `TGA`, `Netpbm`, `SVG`) with zoom, pan, rotate, flip, checkered transparency & EXIF info |
| MarkdownViewer | `markdownviewer` | Rendered Markdown with table of contents sidebar, syntax highlighting, formulas, diagrams & raw source toggle |
| CsvViewer | `csvviewer` | Auto-delimited CSV/TSV table preview |
| HtmlViewer | `htmlviewer` | HTML / MHTML / URL shortcut preview with source toggle |
| PdfViewer | `pdfviewer` | Local PDF inspection and preview |
| VideoViewer | `videoviewer` | Audio/video playback & metadata with cover art and sidecar `.lrc` lyrics |
| ArchiveViewer | `archiveviewer` | Browse `.zip`, `.tar.gz`, `.asar`, `.pak` archives with inline entry preview |
| FontViewer | `fontviewer` | TTF/OTF/WOFF pangram and glyph preview |
| TextViewer | `textviewer` | Code/text/patch diff viewer with encoding detection, line numbers, wrap, search & JSON/XML/PLIST pretty-formatting |
| BinaryViewer | `binaryviewer` | Hex dump & structured binary inspector |

### Specialized Developer & Windows Viewers (`devviewers`)

- `PEViewer` (`peviewer`), `ELFViewer` (`elfviewer`), `CertViewer` (`certviewer`), `MailViewer` (`mailviewer`)
- `AppViewer` (`appviewer`), `OfficeViewer` (`officeviewer`, formatted DOCX / XLSX / PPTX), `DbViewer` (`dbviewer` for SQLite)
- `Model3DViewer` (`model3dviewer`), `CLSIDViewer` (`clsidviewer`), `PrefetchViewer` (`prefetchviewer`)
- `DumpViewer` (`dumpviewer`), `ChmViewer` (`chmviewer`), `PluginInstaller` (`plugininstaller`)
- `ThumbnailViewer` (`thumbnailviewer`), `MediaInfoViewer` (`mediainfo`)

### Fallback

- `InfoPanel` (`infopanel`) — file/folder metadata, shell thumbnail, drive usage, and hash inspection.

## Development & Build

### Prerequisites

- **Go**: matches `go.mod` (`go 1.27.3+`)
- **OS**: Windows x64 for running/building the native desktop app (`go tool nsgo test` also runs headless tests)

### Common Commands

```sh
# Run static reactive & catalog checks
go tool nsgo check

# Run the headless test suite
go tool nsgo test

# Launch in development mode
go tool nsgo dev

# Build Windows x64 distributable (outputs to dist/)
go tool nsgo build -target windows_x64
```

### Releases

Push a `v*` tag to build and publish a GitHub Release:

```sh
git tag v0.1.0
git push origin v0.1.0
```

The Release workflow runs `nsgo check` and `nsgo test`, builds the Windows installer, generates the release notes from Conventional Commits, publishes the release, and commits the updated [CHANGELOG.md](CHANGELOG.md) back to `main`. Use a tag such as `v0.1.0-rc.1` for a prerelease.

### CLI Flags

```sh
NSGook.exe [--top] [--pin] <file>...
```

- `--top` / `-top` / `/top`: start opened preview windows Always on Top
- `--pin` / `-pin` / `/pin`: start opened preview windows pinned

## Project Structure

```text
nsgook/
├── main.go              # Entry point & CLI argument parsing
├── nsgo.yaml            # NSGo application & build manifest
├── assets/              # Embedded application icon assets
├── locales/             # English (en) & Simplified Chinese (zh-Hans) catalogs
├── app_test.go          # End-to-end headless UI & memory regression tests
└── internal/
    ├── app/             # Controller, ViewerSession, settings UI, 9-zone placement
    ├── config/          # YAML configuration store & extension allow/block lists
    ├── sys/             # Win32 low-level keyboard hook, shell selection & file watcher
    ├── viewer/          # Plugin interface, registry, and shared viewer.Context
    └── plugins/         # 26 built-in file viewers & shared pluginui components
```
