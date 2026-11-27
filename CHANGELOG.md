# Changelog

All notable changes to NSGook are documented here. This file and each
GitHub release's notes are generated from Conventional Commit messages.

## [0.1.2] - 2026-11-27

### Features
- **ui**: Auto-hide media controls until the pointer moves
- **ui**: Fade the media controls in and out
- **office**: Render DOCX, XLSX, and PPTX previews

### Fixes
- **video**: Play non-ASCII file names natively
- **ui**: Fill the More menu trigger under the pointer
- **ui**: Close the More menu when the preview hides
- **ui**: Ignore a slider release over the media buttons
- **ui**: Remove the volume slider's step ticks

### CI
- **release**: Build the Windows installer without NSIS

### Maintenance
- **deps**: Update nsgo to v0.3.4

## [0.1.1] - 2026-11-24

### Fixes
- **appviewer**: Compose adaptive launcher icons

### Performance
- **pe**: Paint the report before verifying the signature
- **pe**: Show the summary before imports and the icon
- **archive**: Stream tar indexes while scanning
- **pdf**: Reuse inspected metadata between Prepare and View
- **pe**: Read the shell icon without thumbnail handlers

## [0.1.0] - 2026-11-24

### Features
- Initial NSGook implementation
- **mediainfo**: Compute SHA-256 on demand
- **pe**: Show shell file properties, signature status, and icon
- **audio**: Stream audio previews through audio.Player

### Fixes
- **sys**: Read Everything 1.4 selection through its copy IPC
- **pdf**: Serve local files behind a token-only URL
- **preview**: Size windows from the real display and fit tall images
- **text**: Reject binary MDX files and handle truncated encodings
- **archive**: Decode legacy GBK ZIP entry names
- **video**: Fall back to WebView2 when native playback fails

### CI
- **release**: Publish tagged builds and generate the changelog

### Maintenance
- Check text out as LF so generated catalogs match in CI
- Add AGENTS.md
- **deps**: Update nsgo to v0.3.0

