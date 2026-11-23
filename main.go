// Package main launches NSGook, a pure-Go QuickLook rewrite powered by NSGo.
package main

import (
	"os"
	"strings"

	"github.com/nsgo-dev/nsgo"
	"github.com/nsgo-dev/nsgo/core"

	"nsgook/internal/app"
)

func main() {
	opts := parseCLIArgs(os.Args[1:])
	nsgo.Run(func(cx *core.Scope) {
		app.Start(cx, opts)
	}, nsgo.HideDock(), nsgo.InstanceKey("nsgook"))
}

func parseCLIArgs(args []string) app.AppOptions {
	var opts app.AppOptions
	for _, a := range args {
		low := strings.ToLower(strings.TrimSpace(a))
		switch low {
		case "":
			continue
		case "--top", "/top", "-top":
			opts.InitialTopmost = true
		case "--pin", "/pin", "-pin":
			opts.InitialPin = true
		default:
			if strings.HasPrefix(a, "-") || strings.HasPrefix(a, "/") {
				// Ignore unknown switch flags gently like QuickLook.
				if _, err := os.Stat(a); err != nil {
					continue
				}
			}
			opts.InitialFiles = append(opts.InitialFiles, a)
		}
	}
	return opts
}
