// Package assets embeds NSGook's bundled binary assets.
package assets

import _ "embed"

// AppIconPNG is the 256x256 PNG application and tray icon.
//
//go:embed Icon.png
var AppIconPNG []byte
