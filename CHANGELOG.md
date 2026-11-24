# Changelog

All notable changes to NSGook are documented here. This file and each
GitHub release's notes are generated from Conventional Commit messages.

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

