package officehtml

import (
	"fmt"
	"html"
	"path/filepath"
	"strings"

	"github.com/xuri/excelize/v2"
)

const (
	xlsxMaxRows      = 1200
	xlsxMaxCols      = 80
	xlsxStyleBudget  = 20000
	xlsxDefaultRowPx = 20.0
)

type xlsxCellStyle struct {
	bg, fg                  string
	bold, italic, underline bool
	strike                  bool
	size                    float64
	align, valign           string
	wrap                    bool
	borders                 [4]string // top, right, bottom, left
}

func renderXLSX(path string, labels Labels) (string, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return "", fmt.Errorf("officehtml: %s has no worksheets", filepath.Base(path))
	}

	var body strings.Builder
	body.WriteString(`<div class="book">`)
	body.WriteString(`<nav class="tabs">`)
	for i, sheet := range sheets {
		fmt.Fprintf(&body, `<a href="#sheet-%d">%s</a>`, i, html.EscapeString(sheet))
	}
	body.WriteString(`</nav>`)

	rendered := 0
	styles := map[int]xlsxCellStyle{}
	budget := xlsxStyleBudget
	for i, sheet := range sheets {
		rows, truncatedRows, truncatedCols, err := readXLSXRows(f, sheet)
		if err != nil {
			continue
		}
		emptySheet := labels.EmptySheet
		if emptySheet == "" {
			emptySheet = "Empty sheet"
		}
		if len(rows) == 0 {
			fmt.Fprintf(&body, `<section class="sheet" id="sheet-%d"><h2>%s</h2><p class="note">%s</p></section>`, i, html.EscapeString(sheet), html.EscapeString(emptySheet))
			rendered++
			continue
		}
		rendered++
		maxCols := 0
		for _, row := range rows {
			if len(row) > maxCols {
				maxCols = len(row)
			}
		}
		if maxCols == 0 {
			fmt.Fprintf(&body, `<section class="sheet" id="sheet-%d"><h2>%s</h2><p class="note">%s</p></section>`, i, html.EscapeString(sheet), html.EscapeString(emptySheet))
			continue
		}

		widths := make([]float64, maxCols)
		for c := 0; c < maxCols; c++ {
			name, _ := excelize.ColumnNumberToName(c + 1)
			w, _ := f.GetColWidth(sheet, name)
			if w <= 0 {
				w = 8.43
			}
			px := w*7 + 5
			widths[c] = min(max(px, 24), 360)
		}

		type span struct{ rows, cols int }
		merges := map[[2]int]span{}
		covered := map[[2]int]bool{}
		if cells, err := f.GetMergeCells(sheet, true); err == nil {
			if len(cells) > 4000 {
				cells = cells[:4000]
			}
			for _, mc := range cells {
				c1, r1, err1 := excelize.CellNameToCoordinates(mc.GetStartAxis())
				c2, r2, err2 := excelize.CellNameToCoordinates(mc.GetEndAxis())
				if err1 != nil || err2 != nil || r1 < 1 || c1 < 1 {
					continue
				}
				rs, cs := r2-r1+1, c2-c1+1
				if rs < 1 || cs < 1 {
					continue
				}
				merges[[2]int{r1 - 1, c1 - 1}] = span{rows: rs, cols: cs}
				for r := r1; r <= r2; r++ {
					for c := c1; c <= c2; c++ {
						if r != r1 || c != c1 {
							covered[[2]int{r - 1, c - 1}] = true
						}
					}
				}
			}
		}

		fmt.Fprintf(&body, `<section class="sheet" id="sheet-%d">`, i)
		fmt.Fprintf(&body, `<h2>%s <span class="dim">%s</span></h2>`, html.EscapeString(sheet), html.EscapeString(sheetDimension(rows, maxCols)))
		body.WriteString(`<div class="grid-wrap"><table><colgroup><col style="width:46px">`)
		for _, w := range widths {
			fmt.Fprintf(&body, `<col style="width:%.0fpx">`, w)
		}
		body.WriteString(`</colgroup><thead><tr><th class="corner"></th>`)
		for c := 0; c < maxCols; c++ {
			name, _ := excelize.ColumnNumberToName(c + 1)
			fmt.Fprintf(&body, `<th>%s</th>`, name)
		}
		body.WriteString(`</tr></thead><tbody>`)

		for ri, row := range rows {
			body.WriteString(`<tr`)
			if h, err := f.GetRowHeight(sheet, ri+1); err == nil && h > 0 {
				fmt.Fprintf(&body, ` style="height:%.1fpx"`, h*96/72)
			}
			body.WriteString(`>`)
			fmt.Fprintf(&body, `<th class="rowhead">%d</th>`, ri+1)
			for ci := 0; ci < maxCols; ci++ {
				if covered[[2]int{ri, ci}] {
					continue
				}
				value := ""
				if ci < len(row) {
					value = row[ci]
				}
				sp, merged := merges[[2]int{ri, ci}]
				if !merged && value == "" {
					// Skip cells that are empty and unremarkable to keep
					// the page small; keep the ones with a style.
				}
				cs := xlsxCellStyle{}
				if budget > 0 && (value != "" || merged) {
					axis, _ := excelize.CoordinatesToCellName(ci+1, ri+1)
					if id, err := f.GetCellStyle(sheet, axis); err == nil {
						if cached, ok := styles[id]; ok {
							cs = cached
						} else if st, err := f.GetStyle(id); err == nil {
							cs = convertXLSXStyle(st)
							styles[id] = cs
						}
						budget--
					}
				}
				if value == "" && !merged && cs.bg == "" && cs.borders == [4]string{} {
					body.WriteString(`<td></td>`)
					continue
				}
				body.WriteString(`<td`)
				if merged {
					if sp.cols > 1 {
						fmt.Fprintf(&body, ` colspan="%d"`, min(sp.cols, maxCols-ci))
					}
					if sp.rows > 1 {
						fmt.Fprintf(&body, ` rowspan="%d"`, min(sp.rows, len(rows)-ri))
					}
				}
				var css []string
				if cs.bg != "" {
					css = append(css, "background:"+cs.bg)
				}
				if cs.fg != "" {
					css = append(css, "color:"+cs.fg)
				}
				if cs.bold {
					css = append(css, "font-weight:700")
				}
				if cs.italic {
					css = append(css, "font-style:italic")
				}
				if cs.underline {
					css = append(css, "text-decoration:underline")
				}
				if cs.strike {
					if cs.underline {
						css = append(css, "text-decoration:underline line-through")
					} else {
						css = append(css, "text-decoration:line-through")
					}
				}
				if cs.size > 0 {
					css = append(css, fmt.Sprintf("font-size:%.1fpx", cs.size))
				}
				if cs.align != "" {
					css = append(css, "text-align:"+cs.align)
				}
				if cs.valign != "" {
					css = append(css, "vertical-align:"+cs.valign)
				}
				if cs.wrap {
					css = append(css, "white-space:pre-wrap;overflow-wrap:break-word")
				}
				sides := []string{"top", "right", "bottom", "left"}
				for k, side := range sides {
					if cs.borders[k] != "" {
						css = append(css, "border-"+side+":"+cs.borders[k])
					}
				}
				if len(css) > 0 {
					fmt.Fprintf(&body, ` style="%s"`, strings.Join(css, ";"))
				}
				body.WriteString(`>`)
				body.WriteString(html.EscapeString(strings.TrimRight(value, "\r\n")))
				body.WriteString(`</td>`)
			}
			body.WriteString(`</tr>`)
		}
		body.WriteString(`</tbody></table></div>`)
		if truncatedRows || truncatedCols {
			var notes []string
			if truncatedRows {
				if labels.RowsLimit != nil {
					notes = append(notes, labels.RowsLimit(xlsxMaxRows))
				} else {
					notes = append(notes, fmt.Sprintf("Only the first %d rows are shown", xlsxMaxRows))
				}
			}
			if truncatedCols {
				if labels.ColsLimit != nil {
					notes = append(notes, labels.ColsLimit(xlsxMaxCols))
				} else {
					notes = append(notes, fmt.Sprintf("Only the first %d columns are shown", xlsxMaxCols))
				}
			}
			fmt.Fprintf(&body, `<p class="note">%s</p>`, html.EscapeString(strings.Join(notes, " · ")))
		}
		body.WriteString(`</section>`)
	}
	if rendered == 0 {
		return "", fmt.Errorf("officehtml: %s has no readable worksheets", filepath.Base(path))
	}
	body.WriteString(`</div>`)

	css := `
.book{padding:14px 16px 40px;max-width:100%}
.tabs{position:sticky;top:0;z-index:5;display:flex;gap:6px;overflow-x:auto;padding:8px 2px 10px;background:#e8e8ec}
.tabs a{padding:4px 12px;border-radius:6px;background:#fff;box-shadow:0 1px 2px rgba(0,0,0,.12);text-decoration:none;font-size:13px;white-space:nowrap}
.sheet{margin:8px 0 30px}
.sheet h2{font-size:15px;font-weight:600;margin:10px 2px 8px}
.sheet .dim{color:#8a8f98;font-weight:400;font-size:12px;margin-left:8px}
.grid-wrap{display:inline-block;vertical-align:top;max-width:100%;overflow:auto;max-height:calc(100vh - 110px);border:1px solid #d7d7db;border-radius:8px;background:#fff}
table{border-collapse:separate;border-spacing:0;table-layout:fixed;background:#fff;font-size:13px}
th,td{border-right:1px solid #e3e3e7;border-bottom:1px solid #e3e3e7;padding:3px 6px;text-align:left;vertical-align:bottom;overflow:hidden;white-space:nowrap;text-overflow:clip;height:24px}
thead th{position:sticky;top:0;z-index:3;background:#f3f3f5;color:#6b7280;font-weight:500;text-align:center;font-size:11px;padding:5px 0}
th.rowhead{position:sticky;left:0;z-index:2;background:#f3f3f5;color:#8a8f98;font-weight:400;text-align:center;font-size:11px;padding:0 8px}
th.corner{position:sticky;left:0;top:0;z-index:4;background:#ececef;width:46px}
tbody tr:nth-child(even) td{background-color:#fcfcfd}
tbody td{background:#fff}
.note{color:#8a8f98;font-size:12px;margin:8px 2px}
@media(max-width:760px){.book{padding:8px 8px 30px}.grid-wrap{max-height:none}}
`
	return document(filepath.Base(path), css, body.String()), nil
}

// readXLSXRows streams a sheet so a million-row used range does not fill
// memory before the preview caps it.
func readXLSXRows(f *excelize.File, sheet string) (rows [][]string, truncatedRows, truncatedCols bool, err error) {
	it, err := f.Rows(sheet)
	if err != nil {
		return nil, false, false, err
	}
	defer it.Close()
	for it.Next() {
		if len(rows) >= xlsxMaxRows {
			truncatedRows = true
			break
		}
		row, err := it.Columns()
		if err != nil {
			continue
		}
		if len(row) > xlsxMaxCols {
			row = row[:xlsxMaxCols]
			truncatedCols = true
		}
		rows = append(rows, row)
	}
	return rows, truncatedRows, truncatedCols, nil
}

func convertXLSXStyle(st *excelize.Style) xlsxCellStyle {
	var out xlsxCellStyle
	if st.Font != nil {
		out.bold = st.Font.Bold
		out.italic = st.Font.Italic
		out.underline = st.Font.Underline != ""
		out.strike = st.Font.Strike
		if st.Font.Size > 0 {
			out.size = st.Font.Size * 96 / 72
		}
		out.fg = xlsxColor(st.Font.Color)
		if out.fg == "" && st.Font.ColorTheme != nil {
			out.fg = xlsxThemeColor(*st.Font.ColorTheme)
		}
	}
	if st.Fill.Type != "" && len(st.Fill.Color) > 0 {
		// Pattern colors are the foreground first and the background after.
		c := st.Fill.Color[0]
		if len(st.Fill.Color) > 1 && xlsxColor(c) == "" {
			c = st.Fill.Color[1]
		}
		if xlsxColor(c) != "" {
			out.bg = xlsxColor(c)
		}
	}
	if st.Alignment != nil {
		switch strings.ToLower(st.Alignment.Horizontal) {
		case "center", "centercontinuous", "distributed":
			out.align = "center"
		case "right":
			out.align = "right"
		case "justify":
			out.align = "justify"
		}
		switch strings.ToLower(st.Alignment.Vertical) {
		case "center":
			out.valign = "middle"
		case "top":
			out.valign = "top"
		case "bottom":
			out.valign = "bottom"
		}
		out.wrap = st.Alignment.WrapText
	}
	// Border order is left, right, top, bottom, diagonal.
	sides := [4]int{2, 3, 1, 0}
	for k, idx := range sides {
		if idx >= len(st.Border) {
			continue
		}
		bd := st.Border[idx]
		if bd.Style == 0 {
			continue
		}
		color := xlsxColor(bd.Color)
		if color == "" {
			color = "#9ca3af"
		}
		out.borders[k] = "1px solid " + color
	}
	return out
}

func xlsxColor(c string) string {
	c = strings.TrimSpace(c)
	if c == "" {
		return ""
	}
	if strings.HasPrefix(c, "#") {
		if len(c) == 7 {
			return c
		}
		if len(c) == 9 {
			return "#" + c[3:]
		}
		return ""
	}
	if len(c) == 8 {
		return "#" + c[2:]
	}
	if len(c) == 6 {
		return "#" + c
	}
	return ""
}

func xlsxThemeColor(index int) string {
	colors := []string{"#FFFFFF", "#000000", "#E7E6E6", "#44546A", "#4472C4", "#ED7D31", "#A5A5A5", "#FFC000", "#5B9BD5", "#70AD47"}
	if index >= 0 && index < len(colors) {
		return colors[index]
	}
	return ""
}

func sheetDimension(rows [][]string, cols int) string {
	if len(rows) == 0 || cols == 0 {
		return ""
	}
	last, _ := excelize.CoordinatesToCellName(cols, len(rows))
	return "A1:" + last
}
