package pluginui

import (
	"github.com/nsgo-dev/nsgo/base/datatable"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// DataTableStyles returns themed styles for base/datatable.DataTable.
func DataTableStyles(cx *core.Scope) func() datatable.Styles {
	return ui.Themed(cx, func() datatable.Styles {
		colors := ui.ColorsOf(cx).Now()
		selected := &core.Style{Color: colors.Text, Opacity: core.Alpha(1)}
		chevron := core.Style{Color: colors.Muted, Opacity: core.Alpha(0.6), Selected: selected}
		return datatable.Styles{
			Header:             core.Style{Background: ui.Alpha(colors.Inset, 0.7), Color: colors.Muted, Borders: core.Edges{Bottom: 1}, BorderColor: colors.Border},
			HeaderCell:         core.Style{Padding: core.Edges{Left: 8, Right: 4}},
			HeaderContent:      core.Style{Gap: 4},
			HeaderCellSelected: core.Style{Background: ui.Alpha(colors.Accent, 0.10)},
			HeaderCellDragging: core.Style{Opacity: core.Alpha(0.5)},
			GroupCell:          core.Style{Borders: core.Edges{Bottom: 1, Right: 1}, BorderColor: colors.Border, Padding: core.Symmetric(0, 8)},
			SortIndicator:      core.Style{CornerRadius: 4, Color: colors.Muted, Hover: &core.Style{Background: ui.Alpha(colors.Text, 0.10)}},
			SortAscending:      chevron,
			SortDescending:     chevron,
			ResizeHandle:       core.Style{Background: colors.Border},
			MoveIndicator:      core.Style{Background: colors.Accent},
			Row:                core.Style{Borders: core.Edges{Bottom: 1}, BorderColor: colors.Line},
			RowOdd:             core.Style{Background: ui.Alpha(colors.Inset, 0.35)},
			RowSelected:        core.Style{Background: ui.Alpha(colors.Accent, 0.12)},
			RowActive:          core.Style{Background: ui.Alpha(colors.Inset, 0.6)},
			Cell:               core.Style{Padding: core.Symmetric(0, 8)},
			CellSelected:       core.Style{BorderWidth: 1, BorderColor: colors.Accent},
			CellColumnSelected: core.Style{Background: ui.Alpha(colors.Accent, 0.08)},
			CellActive:         core.Style{BorderWidth: 1, BorderColor: ui.Alpha(colors.Accent, 0.5)},
			RowHeader:          core.Style{Color: colors.Muted},
			FixedEdge:          core.Style{Background: colors.Border},
		}
	})
}
