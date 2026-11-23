// Package csvviewer implements NSGook's virtualized CSV, TSV, and PSV spreadsheet viewer.
package csvviewer

import (
	"cmp"
	"encoding/csv"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/nsgo-dev/nsgo/base/datatable"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"nsgook/internal/plugins/pluginui"
	"nsgook/internal/plugins/textviewer"
	"nsgook/internal/viewer"
	"nsgook/locales"
)

// Plugin previews delimiter-separated tabular files (.csv, .tsv, .psv).
type Plugin struct{}

func New() viewer.Plugin { return Plugin{} }

func (Plugin) ID() string    { return "csvviewer" }
func (Plugin) Name() string  { return "CsvViewer" }
func (Plugin) Priority() int { return 0 }

func (Plugin) CanHandle(path string) bool {
	if path == "" || strings.HasPrefix(path, "::") {
		return false
	}
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".csv" || ext == ".tsv" || ext == ".psv"
}

func (Plugin) Prepare(path string, ctx *viewer.Context) {
	ctx.PreferredSize = core.Size{Width: 920, Height: 620}
	ctx.Title.Set(viewer.DisplayName(path))
}

// TableData holds parsed tabular records and detected delimiter.
type TableData struct {
	Rows      [][]string
	NumCols   int
	Delimiter rune
}

// ParseCSVBytes decodes and parses CSV/TSV/PSV data with automatic delimiter detection.
func ParseCSVBytes(ext string, raw []byte) (TableData, error) {
	txt, _ := textviewer.DecodeText(raw)
	delim := detectDelimiter(ext, txt)

	r := csv.NewReader(strings.NewReader(txt))
	r.Comma = delim
	r.LazyQuotes = true
	r.FieldsPerRecord = -1

	records, err := r.ReadAll()
	if err != nil && len(records) == 0 {
		return TableData{}, err
	}
	maxCols := 0
	for _, row := range records {
		if len(row) > maxCols {
			maxCols = len(row)
		}
	}
	if maxCols == 0 {
		maxCols = 1
	}
	for i := range records {
		for len(records[i]) < maxCols {
			records[i] = append(records[i], "")
		}
	}
	return TableData{Rows: records, NumCols: maxCols, Delimiter: delim}, nil
}

func detectDelimiter(ext, txt string) rune {
	switch strings.ToLower(ext) {
	case ".tsv":
		return '\t'
	case ".psv":
		return '|'
	}
	sample := txt
	if len(sample) > 8192 {
		sample = sample[:8192]
	}
	lines := strings.Split(strings.ReplaceAll(sample, "\r\n", "\n"), "\n")
	if len(lines) > 12 {
		lines = lines[:12]
	}
	best := ','
	bestScore := -1
	for _, cand := range []rune{',', '\t', ';', '|'} {
		s := string(cand)
		counts := make([]int, 0, len(lines))
		for _, l := range lines {
			if strings.TrimSpace(l) == "" {
				continue
			}
			c := strings.Count(l, s)
			if c > 0 {
				counts = append(counts, c)
			}
		}
		if len(counts) == 0 {
			continue
		}
		// Prefer delimiters that appear consistently across lines.
		score := len(counts)*100 + counts[0]
		first := counts[0]
		for _, c := range counts[1:] {
			if c != first {
				score -= 30
			}
		}
		if score > bestScore {
			bestScore = score
			best = cand
		}
	}
	return best
}

func (Plugin) View(cx *core.Scope, path string, ctx *viewer.Context) core.Node {
	colors := ui.ColorsOf(cx)
	tr := locales.For(cx)

	parsed := core.NewSignalFunc(cx, TableData{}, func(_, _ TableData) bool { return false })
	loadErr := core.NewSignal(cx, "")
	hasHeader := core.NewSignal(cx, true)
	filter := core.NewSignal(cx, "")
	sortState := core.NewSignal(cx, datatable.Sort{})

	var stopped atomic.Bool
	cx.Cleanup(func() {
		stopped.Store(true)
		parsed.Set(TableData{})
	})

	ctx.IsBusy.Set(true)
	go func() {
		raw, err := os.ReadFile(path)
		if stopped.Load() {
			return
		}
		var td TableData
		if err == nil {
			td, err = ParseCSVBytes(filepath.Ext(path), raw)
		}
		cx.Post(func() {
			if stopped.Load() {
				return
			}
			ctx.IsBusy.Set(false)
			if err != nil {
				loadErr.Set(err.Error())
				return
			}
			parsed.Set(td)
		})
	}()

	headers := core.NewMemoFunc(cx, func() []string {
		td := parsed.Get()
		out := make([]string, td.NumCols)
		if hasHeader.Get() && len(td.Rows) > 0 {
			for i := 0; i < td.NumCols; i++ {
				h := strings.TrimSpace(td.Rows[0][i])
				if h == "" {
					h = columnLetter(i)
				}
				out[i] = h
			}
			return out
		}
		for i := 0; i < td.NumCols; i++ {
			out[i] = columnLetter(i)
		}
		return out
	}, slices.Equal)

	columns := core.NewMemoFunc(cx, func() []datatable.Column {
		hdrs := headers.Get()
		td := parsed.Get()
		cols := make([]datatable.Column, len(hdrs))
		for i, h := range hdrs {
			w := core.Points(130)
			if len(h) > 16 {
				w = 180
			}
			// Sample first 20 rows to size columns sensibly.
			for r := 0; r < min(len(td.Rows), 20); r++ {
				if l := len(td.Rows[r][i]); l > 18 {
					w = max(w, min(core.Points(l*7+24), 300))
				}
			}
			cols[i] = datatable.Column{
				Key:      strconv.Itoa(i),
				Title:    h,
				Width:    w,
				Sortable: true,
			}
		}
		return cols
	}, func(_, _ []datatable.Column) bool { return false })

	viewRows := core.NewMemoFunc(cx, func() [][]string {
		td := parsed.Get()
		start := 0
		if hasHeader.Get() && len(td.Rows) > 0 {
			start = 1
		}
		if start >= len(td.Rows) {
			return nil
		}
		q := strings.ToLower(strings.TrimSpace(filter.Get()))
		rows := make([][]string, 0, len(td.Rows)-start)
		for _, r := range td.Rows[start:] {
			if q == "" {
				rows = append(rows, r)
				continue
			}
			matched := false
			for _, cell := range r {
				if strings.Contains(strings.ToLower(cell), q) {
					matched = true
					break
				}
			}
			if matched {
				rows = append(rows, r)
			}
		}
		s := sortState.Get()
		if s.Key != "" {
			if colIdx, err := strconv.Atoi(s.Key); err == nil && colIdx >= 0 && colIdx < td.NumCols {
				slices.SortStableFunc(rows, func(a, b []string) int {
					c := compareCells(a[colIdx], b[colIdx])
					if s.Descending {
						return -c
					}
					return c
				})
			}
		}
		return rows
	}, func(_, _ [][]string) bool { return false })

	ctx.MoreItems.Set([]viewer.MenuAction{
		{
			ID:       "csv-header",
			Label:    tr.CsvHasHeader(),
			Icon:     lucide.TableProperties,
			Checked:  hasHeader.Get,
			OnSelect: func() { hasHeader.Set(!hasHeader.GetUntracked()) },
		},
	})

	toolbar := pluginui.Toolbar(cx,
		ui.IconOf(lucide.Search, 14, colors.Muted),
		ui.Input{
			Value:       filter.Get,
			OnChange:    filter.Set,
			Placeholder: locales.For(cx).CsvFilterPlaceholder(),
			Style:       core.Style{Width: core.Pt(220)},
		},
		ui.Spacer{},
		ui.Checkbox{
			Text:     locales.For(cx).CsvHasHeader(),
			Checked:  hasHeader.Get,
			OnChange: hasHeader.Set,
		},
	)

	table := datatable.DataTable{
		Columns:  columns.Get,
		RowCount: func() int { return len(viewRows.Get()) },
		Text: func(row, col int) string {
			rows := viewRows.Get()
			if row < 0 || row >= len(rows) || col < 0 || col >= len(rows[row]) {
				return ""
			}
			return rows[row][col]
		},
		Sort:         sortState.Get,
		OnSortChange: sortState.Set,
		RowHeader:    true,
		Stripe:       func() bool { return true },
		RowHeight:    30,
		HeaderHeight: 34,
		StylesFunc:   pluginui.DataTableStyles(cx),
		Style:        core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), MinHeight: core.Pt(0)},
	}

	status := pluginui.StatusBar(cx,
		core.Text{
			ContentFunc: func() string {
				return locales.For(cx).CsvStats(strconv.Itoa(len(viewRows.Get())), strconv.Itoa(parsed.Get().NumCols))
			},
			Style:     core.Style{FontSize: 11},
			StyleFunc: ui.Colored(colors.Muted),
		},
		ui.Spacer{},
		ui.Badge{
			TextFunc: func() string {
				switch parsed.Get().Delimiter {
				case '\t':
					return "TSV"
				case '|':
					return "PSV"
				case ';':
					return "CSV (;)"
				default:
					return "CSV"
				}
			},
			Variant: ui.BadgeNeutral,
		},
	)

	return pluginui.Fill(
		core.Show{
			When: func() bool { return loadErr.Get() != "" },
			Then: func(cx *core.Scope) core.Node {
				return pluginui.EmptyState(cx, lucide.FileExclamationPoint, locales.For(cx).CommonError(), loadErr.Get())
			},
			Else: func(*core.Scope) core.Node {
				return pluginui.Fill(toolbar, table, status)
			},
		},
	)
}

func columnLetter(idx int) string {
	var out []byte
	n := idx
	for {
		out = append([]byte{byte('A' + (n % 26))}, out...)
		n = n/26 - 1
		if n < 0 {
			break
		}
	}
	return string(out)
}

func compareCells(a, b string) int {
	fa, errA := strconv.ParseFloat(strings.TrimSpace(a), 64)
	fb, errB := strconv.ParseFloat(strings.TrimSpace(b), 64)
	if errA == nil && errB == nil {
		return cmp.Compare(fa, fb)
	}
	return strings.Compare(strings.ToLower(a), strings.ToLower(b))
}
