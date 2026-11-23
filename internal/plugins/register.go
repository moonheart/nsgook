// Package plugins registers all built-in NSGook file viewer plugins in priority order.
package plugins

import (
	"nsgook/internal/plugins/archiveviewer"
	"nsgook/internal/plugins/binaryviewer"
	"nsgook/internal/plugins/csvviewer"
	"nsgook/internal/plugins/devviewers"
	"nsgook/internal/plugins/fontviewer"
	"nsgook/internal/plugins/htmlviewer"
	"nsgook/internal/plugins/imageviewer"
	"nsgook/internal/plugins/infopanel"
	"nsgook/internal/plugins/markdownviewer"
	"nsgook/internal/plugins/pdfviewer"
	"nsgook/internal/plugins/textviewer"
	"nsgook/internal/plugins/videoviewer"
	"nsgook/internal/viewer"
)

// NewDefaultRegistry constructs the complete built-in NSGook plugin registry with InfoPanel as fallback.
func NewDefaultRegistry() *viewer.Registry {
	all := []viewer.Plugin{
		imageviewer.New(),
		markdownviewer.New(),
		csvviewer.New(),
		htmlviewer.New(),
		pdfviewer.New(),
		videoviewer.New(),
		archiveviewer.New(),
		fontviewer.New(),
		textviewer.New(),
		binaryviewer.New(),
	}
	all = append(all, devviewers.All()...)
	return viewer.NewRegistry(infopanel.New(), all...)
}
