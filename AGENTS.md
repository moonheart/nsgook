# AGENTS.md

Guidance for coding agents working in `nsgook`.

## Overview & Boundaries

- **NSGook** is a pure-Go [QuickLook](https://github.com/QL-Win/QuickLook) rewrite for Windows built on `github.com/nsgo-dev/nsgo` (see `go.mod`).
- **Do not modify `nsgo` itself** (e.g., any local `../nsgo` checkout). `nsgook` depends on the published `github.com/nsgo-dev/nsgo` module; any local `nsgo` repository is strictly read-only reference.
- Keep `go.mod` module name as `nsgook` unless explicitly asked to change it.

## Build, Check & Test Commands

Always use `go tool nsgo` rather than raw `go test` / `go build` / `go vet`, and run commands **sequentially** (never in parallel in the same workspace):

```pwsh
# Static reactive rules, i18n catalog checks, and go vet
go tool nsgo check

# Headless UI, plugin, and memory-regression tests
go tool nsgo test ./...

# Development mode
go tool nsgo dev

# Build Windows x64 executable (outputs to dist/)
go tool nsgo build -target windows_x64

# Regenerate locales/catalog_gen.go after editing locales/*.yaml
go generate ./locales
```

Notes:
- Format Go files with `gofmt -w` before finishing edits.
- Never force-kill a user-running `NSGook.exe` if `build` reports `Access is denied`; report the lock cleanly instead.
- Do not bypass `commit.gpgsign` when using Git.

## Repository Structure

- `main.go` — Entry point (`nsgo.Run`), CLI flag parsing (`--top`, `--pin`, file paths).
- `nsgo.yaml` — NSGo app manifest and build targets.
- `app_test.go` — End-to-end headless tests (`nsgotest`) covering sessions, shortcuts, settings, and bounded-heap image/file-switching regressions.
- `internal/app/` — `Controller` (`controller.go`), `ViewerSession` & viewer chrome (`viewer_window.go`), Settings UI (`settings_window.go`), and 9-zone window placement (`positioning.go`).
- `internal/config/` — YAML configuration persistence (`config.go`) and extension allowlist/blocklist filtering.
- `internal/sys/` — Win32 low-level keyboard hook (`hook_windows.go`), keystroke state machine (`dispatcher.go`), foreground focus monitor (`provider.go`), shell selection & file watcher (`shell_windows.go`, `actions_windows.go`, `com_windows.go`), and non-Windows stubs (`sys_other.go`).
- `internal/viewer/` — `Plugin` interface, `Registry`, and `Context` (`viewer.go`).
- `internal/plugins/` — Built-in plugin registration (`register.go`), shared UI primitives (`pluginui/`), 10 core plugins, and `devviewers/` (15 specialized plugins).
- `locales/` — `en.yaml`, `zh-Hans.yaml`, and generated `catalog_gen.go` (do not hand-edit `catalog_gen.go`).
- `assets/` — Embedded application icon (`embed.go`, `Icon.png`).

## NSGo & Memory-Safety Conventions

1. **Hidden Window Reuse (`c.hidden`)**:
   - `Controller.CloseActive()` unloads preview content and hides/reuses the unpinned `ViewerSession` window rather than destroying and recreating desktop windows on every `Space` toggle. Keep this behavior to avoid native DXGI/DirectComposition handle churn.
2. **Two-Phase Media Release (`viewer.Context.Releasing`)**:
   - Any plugin that renders `core.Image` must:
     - Call `ctx.TrackMedia(cx)` during `View`.
     - React to `ctx.IsReleasing()` via `cx.Effect` to set `stopped.Store(true)` (and invalidate heavy memos such as `transformed` in `imageviewer`).
     - Use `SourceFunc` (not static `Source`) on `core.Image` and return `core.ImageSource{}` immediately when `ctx.IsReleasing()` is true.
   - `ViewerSession.scheduleContentMount` keeps a mounted media plugin alive for 1 tick with `Releasing=true` so `Tree.Commit()` commits an empty `ImageSource{}` (clearing internal image buffers and native D2D bitmaps) before unmounting on the next tick.
3. **Bypass `imageload.cache` for User Files**:
   - In `imageviewer`, pass file bytes via `core.ImageSource{Data: ...}` instead of `core.ImageSource{URI: file://...}` for native formats so `nsgo`'s 32 MB global `imageload.cache` is not pinned across previews.
4. **Cleanup & Scope Ownership**:
   - Always register `cx.Cleanup` to set `stopped.Store(true)` and clear heavy payload signals (e.g. `[]byte`, `*image.RGBA`, `string` contents, parsed structs).
   - Guard all background goroutines and `cx.Post` callbacks with `stopped.Load()`.
   - Open independent windows (like `Settings`) on the persistent `c.app` scope (`c.app.Run(...)`), never inside transient popup/menu scopes.
5. **UI Primitives**:
   - Prefer `pluginui.IconButton` over `ui.IconButton` inside dynamic plugin views/toolbars that unmount frequently, to avoid retained hoisted tooltip overlay nodes.
   - Use `withLayoutSlotFlush` on `contentBody` branches in `viewer_window.go` so `core.Tree.laidOut` tail slots are overwritten when switching plugins.
   - Configure `editor.Editor` with `NoFolding: true` and `NoIndentGuides: true` where appropriate, and wait for initial file load before mounting the editor.
