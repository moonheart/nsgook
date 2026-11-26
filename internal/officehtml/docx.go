package officehtml

import (
	"archive/zip"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// ---------- WordprocessingML model ----------

type dxVal struct {
	Val string `xml:"val,attr"`
}

type dxIntVal struct {
	Val int `xml:"val,attr"`
}

type dxShd struct {
	Val  string `xml:"val,attr"`
	Fill string `xml:"fill,attr"`
}

type dxFonts struct {
	Ascii    string `xml:"ascii,attr"`
	HAnsi    string `xml:"hAnsi,attr"`
	EastAsia string `xml:"eastAsia,attr"`
}

type dxRunProps struct {
	Style     dxVal     `xml:"rStyle"`
	Bold      *dxVal    `xml:"b"`
	Italic    *dxVal    `xml:"i"`
	Underline *dxVal    `xml:"u"`
	Strike    *dxVal    `xml:"strike"`
	Size      *dxIntVal `xml:"sz"`
	Color     *dxVal    `xml:"color"`
	Highlight *dxVal    `xml:"highlight"`
	Shd       *dxShd    `xml:"shd"`
	Fonts     *dxFonts  `xml:"rFonts"`
	VertAlign *dxVal    `xml:"vertAlign"`
}

type dxSpacing struct {
	Before int    `xml:"before,attr"`
	After  int    `xml:"after,attr"`
	Line   int    `xml:"line,attr"`
	Rule   string `xml:"lineRule,attr"`
}

type dxIndent struct {
	Left      int `xml:"left,attr"`
	FirstLine int `xml:"firstLine,attr"`
	Hanging   int `xml:"hanging,attr"`
}

type dxNumPr struct {
	NumID dxIntVal `xml:"numId"`
	ILvl  dxIntVal `xml:"ilvl"`
}

type dxParaProps struct {
	Style   dxVal      `xml:"pStyle"`
	Align   dxVal      `xml:"jc"`
	Spacing *dxSpacing `xml:"spacing"`
	Indent  *dxIndent  `xml:"ind"`
	NumPr   *dxNumPr   `xml:"numPr"`
	Outline *dxIntVal  `xml:"outlineLvl"`
	Shd     *dxShd     `xml:"shd"`
}

type dxPara struct {
	Props dxParaProps
	Runs  []*dxRun
}

type dxRun struct {
	Props dxRunProps
	Text  []dxRunItem
}

type dxRunItem struct {
	Text  string
	Tab   bool
	Break bool
	Image *dxImage
}

type dxImage struct {
	Rel    string
	Width  int64 // EMU
	Height int64
}

type dxDrawing struct {
	Inline *dxDrawingRef `xml:"inline"`
	Anchor *dxDrawingRef `xml:"anchor"`
}

type dxDrawingRef struct {
	Extent struct {
		Cx int64 `xml:"cx,attr"`
		Cy int64 `xml:"cy,attr"`
	} `xml:"extent"`
	Blip struct {
		Embed string `xml:"embed,attr"`
	} `xml:"graphic>graphicData>pic>blipFill>blip"`
}

type dxPict struct {
	Shape struct {
		Image struct {
			ID string `xml:"id,attr"`
		} `xml:"imagedata"`
	} `xml:"shape"`
}

type dxBorder struct {
	Val   string `xml:"val,attr"`
	Size  int    `xml:"sz,attr"`
	Color string `xml:"color,attr"`
}

type dxBorders struct {
	Top     *dxBorder `xml:"top"`
	Bottom  *dxBorder `xml:"bottom"`
	Left    *dxBorder `xml:"left"`
	Right   *dxBorder `xml:"right"`
	InsideH *dxBorder `xml:"insideH"`
	InsideV *dxBorder `xml:"insideV"`
}

type dxTableProps struct {
	Width   *dxIntVal  `xml:"tblW"`
	Borders *dxBorders `xml:"tblBorders"`
	Align   dxVal      `xml:"jc"`
	Shd     *dxShd     `xml:"shd"`
}

type dxTable struct {
	Props dxTableProps `xml:"tblPr"`
	Cols  []dxGridCol  `xml:"tblGrid>gridCol"`
	Rows  []dxRow      `xml:"tr"`
}

type dxGridCol struct {
	W int `xml:"w,attr"`
}

type dxRow struct {
	Height *dxIntVal `xml:"trPr>trHeight"`
	Cells  []dxCell  `xml:"tc"`
}

type dxCellProps struct {
	Span    *dxIntVal  `xml:"gridSpan"`
	VMerge  *dxVal     `xml:"vMerge"`
	Width   *dxIntVal  `xml:"tcW"`
	Shd     *dxShd     `xml:"shd"`
	VAlign  dxVal      `xml:"vAlign"`
	Borders *dxBorders `xml:"tcBorders"`
}

type dxCell struct {
	Props  dxCellProps
	Blocks []dxBlock
}

type dxBlock struct {
	Para  *dxPara
	Table *dxTable
}

type dxSect struct {
	PageSize *struct {
		W int `xml:"w,attr"`
		H int `xml:"h,attr"`
	} `xml:"pgSz"`
	Margin *struct {
		Top    int `xml:"top,attr"`
		Right  int `xml:"right,attr"`
		Bottom int `xml:"bottom,attr"`
		Left   int `xml:"left,attr"`
	} `xml:"pgMar"`
}

// ---------- WordprocessingML parsing ----------

func (p *dxPara) UnmarshalXML(dec *xml.Decoder, start xml.StartElement) error {
	for {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "pPr":
				if err := dec.DecodeElement(&p.Props, &t); err != nil {
					return err
				}
			case "r":
				run := &dxRun{}
				if err := dec.DecodeElement(run, &t); err != nil {
					return err
				}
				p.Runs = append(p.Runs, run)
			case "hyperlink", "ins", "smartTag", "fldSimple", "sdt", "sdtContent":
				if err := p.collectRuns(dec, t); err != nil {
					return err
				}
			default:
				if err := dec.Skip(); err != nil {
					return err
				}
			}
		case xml.EndElement:
			if t.Name == start.Name {
				return nil
			}
		}
	}
}

func (p *dxPara) collectRuns(dec *xml.Decoder, start xml.StartElement) error {
	for {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "r":
				run := &dxRun{}
				if err := dec.DecodeElement(run, &t); err != nil {
					return err
				}
				p.Runs = append(p.Runs, run)
			case "hyperlink", "ins", "smartTag", "fldSimple", "sdt", "sdtContent":
				if err := p.collectRuns(dec, t); err != nil {
					return err
				}
			default:
				if err := dec.Skip(); err != nil {
					return err
				}
			}
		case xml.EndElement:
			if t.Name == start.Name {
				return nil
			}
		}
	}
}

func (r *dxRun) UnmarshalXML(dec *xml.Decoder, start xml.StartElement) error {
	for {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "rPr":
				if err := dec.DecodeElement(&r.Props, &t); err != nil {
					return err
				}
			case "t":
				s, err := elementText(dec, t)
				if err != nil {
					return err
				}
				r.Text = append(r.Text, dxRunItem{Text: s})
			case "tab":
				r.Text = append(r.Text, dxRunItem{Tab: true})
				if err := dec.Skip(); err != nil {
					return err
				}
			case "br", "cr":
				r.Text = append(r.Text, dxRunItem{Break: true})
				if err := dec.Skip(); err != nil {
					return err
				}
			case "drawing":
				var d dxDrawing
				if err := dec.DecodeElement(&d, &t); err != nil {
					return err
				}
				if img := d.image(); img != nil {
					r.Text = append(r.Text, dxRunItem{Image: img})
				}
			case "pict":
				var p dxPict
				if err := dec.DecodeElement(&p, &t); err != nil {
					return err
				}
				if p.Shape.Image.ID != "" {
					r.Text = append(r.Text, dxRunItem{Image: &dxImage{Rel: p.Shape.Image.ID}})
				}
			case "noBreakHyphen":
				r.Text = append(r.Text, dxRunItem{Text: "-"})
				if err := dec.Skip(); err != nil {
					return err
				}
			default:
				if err := dec.Skip(); err != nil {
					return err
				}
			}
		case xml.EndElement:
			if t.Name == start.Name {
				return nil
			}
		}
	}
}

func (d dxDrawing) image() *dxImage {
	ref := d.Inline
	if ref == nil {
		ref = d.Anchor
	}
	if ref == nil || ref.Blip.Embed == "" {
		return nil
	}
	return &dxImage{Rel: ref.Blip.Embed, Width: ref.Extent.Cx, Height: ref.Extent.Cy}
}

func (c *dxCell) UnmarshalXML(dec *xml.Decoder, start xml.StartElement) error {
	blocks, props, err := parseBlocks(dec, start, true)
	if err != nil {
		return err
	}
	c.Blocks = blocks
	c.Props = props
	return nil
}

// parseBlocks reads paragraphs and tables until start's end. For a table
// cell it also reads the cell properties.
func parseBlocks(dec *xml.Decoder, start xml.StartElement, cell bool) ([]dxBlock, dxCellProps, error) {
	var blocks []dxBlock
	var props dxCellProps
	for {
		tok, err := dec.Token()
		if err != nil {
			return blocks, props, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				p := &dxPara{}
				if err := dec.DecodeElement(p, &t); err != nil {
					return blocks, props, err
				}
				blocks = append(blocks, dxBlock{Para: p})
			case "tbl":
				tbl := &dxTable{}
				if err := dec.DecodeElement(tbl, &t); err != nil {
					return blocks, props, err
				}
				blocks = append(blocks, dxBlock{Table: tbl})
			case "tcPr":
				if err := dec.DecodeElement(&props, &t); err != nil {
					return blocks, props, err
				}
			default:
				if err := dec.Skip(); err != nil {
					return blocks, props, err
				}
			}
		case xml.EndElement:
			if t.Name == start.Name {
				return blocks, props, nil
			}
		}
	}
}

func elementText(dec *xml.Decoder, start xml.StartElement) (string, error) {
	var b strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			return b.String(), err
		}
		switch t := tok.(type) {
		case xml.CharData:
			b.Write(t)
		case xml.EndElement:
			if t.Name == start.Name {
				return b.String(), nil
			}
		}
	}
}

type dxStylesXML struct {
	DocDefaults struct {
		RPr dxRunProps  `xml:"rPrDefault>rPr"`
		PPr dxParaProps `xml:"pPrDefault>pPr"`
	} `xml:"docDefaults"`
	Styles []dxStyle `xml:"style"`
}

type dxStyle struct {
	ID      string      `xml:"styleId,attr"`
	Name    dxVal       `xml:"name"`
	BasedOn dxVal       `xml:"basedOn"`
	Type    string      `xml:"type,attr"`
	PPr     dxParaProps `xml:"pPr"`
	RPr     dxRunProps  `xml:"rPr"`
}

type dxNumbering struct {
	Abstract []dxAbstractNum `xml:"abstractNum"`
	Nums     []dxNum         `xml:"num"`
}

type dxAbstractNum struct {
	ID     int     `xml:"abstractNumId,attr"`
	Levels []dxLvl `xml:"lvl"`
}

type dxLvl struct {
	ILvl   int         `xml:"ilvl,attr"`
	Start  *dxIntVal   `xml:"start"`
	NumFmt dxVal       `xml:"numFmt"`
	Text   dxVal       `xml:"lvlText"`
	PPr    dxParaProps `xml:"pPr"`
}

type dxNum struct {
	ID       int   `xml:"numId,attr"`
	Abstract dxVal `xml:"abstractNumId"`
}

type dxRels struct {
	Items []struct {
		ID     string `xml:"Id,attr"`
		Target string `xml:"Target,attr"`
	} `xml:"Relationship"`
}

// ---------- WordprocessingML rendering ----------

type docxRenderer struct {
	zr       *zip.Reader
	files    map[string]*zip.File
	rels     map[string]string
	styles   map[string]*dxStyle
	abstract map[int]map[int]*dxLvl
	numAbs   map[int]int
	counters map[[2]int]int

	defaultRun  dxRunProps
	defaultPara dxParaProps
	defaultSize float64
	defaultFont string

	contentWidth float64
	imageBudget  int64
}

func renderDOCX(path string) (string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	r := &docxRenderer{
		zr:          &zr.Reader,
		files:       map[string]*zip.File{},
		rels:        map[string]string{},
		styles:      map[string]*dxStyle{},
		abstract:    map[int]map[int]*dxLvl{},
		numAbs:      map[int]int{},
		counters:    map[[2]int]int{},
		imageBudget: 24 << 20,
	}
	for _, f := range zr.File {
		r.files[strings.ToLower(f.Name)] = f
	}
	docXML := r.read("word/document.xml")
	if len(docXML) == 0 {
		return "", fmt.Errorf("officehtml: %s is not a Word document", filepath.Base(path))
	}

	var styles dxStylesXML
	if b := r.read("word/styles.xml"); len(b) > 0 {
		_ = xml.Unmarshal(b, &styles)
	}
	r.defaultRun = styles.DocDefaults.RPr
	r.defaultPara = styles.DocDefaults.PPr
	for i := range styles.Styles {
		st := &styles.Styles[i]
		r.styles[st.ID] = st
		r.styles[strings.ToLower(st.ID)] = st
	}
	r.defaultSize = 11 * 96.0 / 72
	r.defaultFont = `"Calibri","Segoe UI","Microsoft YaHei",sans-serif`
	if r.defaultRun.Size != nil && r.defaultRun.Size.Val > 0 {
		r.defaultSize = float64(r.defaultRun.Size.Val) / 2 * 96 / 72
	}
	if r.defaultRun.Fonts != nil {
		if f := firstFont(*r.defaultRun.Fonts); f != "" {
			r.defaultFont = fontStack(f)
		}
	}

	var numbering dxNumbering
	if b := r.read("word/numbering.xml"); len(b) > 0 {
		_ = xml.Unmarshal(b, &numbering)
	}
	for i := range numbering.Abstract {
		a := &numbering.Abstract[i]
		m := map[int]*dxLvl{}
		for j := range a.Levels {
			lv := &a.Levels[j]
			m[lv.ILvl] = lv
		}
		r.abstract[a.ID] = m
	}
	for _, n := range numbering.Nums {
		r.numAbs[n.ID] = atoiOr(n.Abstract.Val, 0)
	}

	var rels dxRels
	if b := r.read("word/_rels/document.xml.rels"); len(b) > 0 {
		_ = xml.Unmarshal(b, &rels)
	}
	for _, rel := range rels.Items {
		r.rels[rel.ID] = rel.Target
	}

	// Section geometry: Letter unless the document says otherwise.
	pageW, marginTop, marginRight, marginBottom, marginLeft := 12240, 1440, 1440, 1440, 1440
	var meta struct {
		Body struct {
			Sect *dxSect `xml:"sectPr"`
		} `xml:"body"`
	}
	_ = xml.Unmarshal(docXML, &meta)
	if s := meta.Body.Sect; s != nil {
		if s.PageSize != nil && s.PageSize.W > 0 {
			pageW = s.PageSize.W
		}
		if s.Margin != nil {
			marginTop, marginRight, marginBottom, marginLeft = s.Margin.Top, s.Margin.Right, s.Margin.Bottom, s.Margin.Left
		}
	}
	widthPx := float64(pageW) / 15
	if widthPx < 480 {
		widthPx = 480
	}
	if widthPx > 1100 {
		widthPx = 1100
	}
	padL, padR := float64(marginLeft)/15, float64(marginRight)/15
	padT, padB := float64(marginTop)/15, float64(marginBottom)/15
	contentW := widthPx - padL - padR
	if contentW < 240 {
		contentW = widthPx - 48
		padL, padR = 24, 24
	}
	r.contentWidth = contentW

	dec := xml.NewDecoder(strings.NewReader(string(docXML)))
	var blocks []dxBlock
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "body" {
			blocks, _, _ = parseBlocks(dec, se, false)
			break
		}
	}

	var body strings.Builder
	body.WriteString(fmt.Sprintf(`<div class="doc" style="width:%.0fpx;padding:%.0fpx %.0fpx %.0fpx %.0fpx">`, widthPx, padT, padR, padB, padL))
	for i := range blocks {
		r.writeBlock(&body, &blocks[i], false)
	}
	body.WriteString(`</div>`)

	title := filepath.Base(path)
	css := fmt.Sprintf(`
.doc{max-width:100%%;margin:22px auto;background:#fff;color:#111;box-shadow:0 8px 30px rgba(0,0,0,.18);font-family:%s;font-size:%.2fpx;line-height:1.42;overflow-wrap:break-word}
.doc p{margin:0}
.doc h1,.doc h2,.doc h3,.doc h4,.doc h5,.doc h6{margin:.65em 0 .3em;line-height:1.25;font-weight:600}
.doc h1{font-size:28px}.doc h2{font-size:24px}.doc h3{font-size:20px}.doc h4{font-size:18px}.doc h5{font-size:16px}.doc h6{font-size:14px}
.doc table{border-collapse:collapse;margin:8px 0;table-layout:fixed}
.doc table td{padding:4px 6px;vertical-align:top}
.doc table.bordered td{border:1px solid #cfcfcf}
.doc table p{margin:1px 0}
.doc img{max-width:100%%;height:auto}
.doc .pagebreak{page-break-after:always;border:0;border-top:1px dashed #ddd;margin:22px 0}
@media(max-width:760px){.doc{width:100%%!important;margin:0 auto;box-shadow:none;padding:24px 18px!important}}
`, r.defaultFont, r.defaultSize)
	return document(title, css, body.String()), nil
}

func (r *docxRenderer) read(name string) []byte {
	f := r.files[strings.ToLower(name)]
	if f == nil {
		return nil
	}
	rc, err := f.Open()
	if err != nil {
		return nil
	}
	defer rc.Close()
	b, _ := io.ReadAll(io.LimitReader(rc, 8<<20))
	return b
}

func (r *docxRenderer) writeBlock(b *strings.Builder, blk *dxBlock, inCell bool) {
	switch {
	case blk.Para != nil:
		r.writePara(b, blk.Para, inCell)
	case blk.Table != nil:
		r.writeTable(b, blk.Table, inCell)
	}
}

type dxRunLook struct {
	bold, italic, underline, strike bool
	size                            float64
	sizeSet                         bool
	color, bg, font, vert           string
}

func (r *docxRenderer) styleChain(id string) []*dxStyle {
	var chain []*dxStyle
	seen := map[string]bool{}
	for id != "" && !seen[id] {
		seen[id] = true
		st := r.styles[id]
		if st == nil {
			st = r.styles[strings.ToLower(id)]
		}
		if st == nil {
			break
		}
		chain = append(chain, st)
		id = st.BasedOn.Val
	}
	slices.Reverse(chain)
	return chain
}

func applyRunProps(look *dxRunLook, p dxRunProps, markSize bool) {
	if p.Bold != nil {
		look.bold = toggleValue(p.Bold.Val)
	}
	if p.Italic != nil {
		look.italic = toggleValue(p.Italic.Val)
	}
	if p.Underline != nil {
		look.underline = !strings.EqualFold(p.Underline.Val, "none")
	}
	if p.Strike != nil {
		look.strike = !strings.EqualFold(p.Strike.Val, "none")
	}
	if p.Size != nil && p.Size.Val > 0 {
		look.size = float64(p.Size.Val) / 2 * 96 / 72
		if markSize {
			look.sizeSet = true
		}
	}
	if p.Color != nil {
		if c := dxColor(p.Color.Val); c != "" {
			look.color = c
		}
	}
	if p.Highlight != nil {
		if c := highlightColor(p.Highlight.Val); c != "" {
			look.bg = c
		}
	}
	if p.Shd != nil {
		if c := dxColor(p.Shd.Fill); c != "" {
			look.bg = c
		}
	}
	if p.Fonts != nil {
		if f := firstFont(*p.Fonts); f != "" {
			look.font = fontStack(f)
		}
	}
	if p.VertAlign != nil {
		switch p.VertAlign.Val {
		case "superscript":
			look.vert = "super"
		case "subscript":
			look.vert = "sub"
		}
	}
}

func (r *docxRenderer) runLook(p dxParaProps, run dxRunProps) dxRunLook {
	look := dxRunLook{size: r.defaultSize, color: "#1a1a1a", font: r.defaultFont}
	applyRunProps(&look, r.defaultRun, false)
	styleID := p.Style.Val
	if styleID == "" {
		styleID = "Normal"
	}
	for _, st := range r.styleChain(styleID) {
		applyRunProps(&look, st.RPr, true)
	}
	if run.Style.Val != "" {
		for _, st := range r.styleChain(run.Style.Val) {
			applyRunProps(&look, st.RPr, true)
		}
	}
	applyRunProps(&look, run, true)
	return look
}

type dxParaLook struct {
	align                string
	before, after        float64
	lineHeight           string
	left, first, hanging float64
	bg                   string
	numID, ilvl          int
	outline              int
	outlineSet           bool
}

func applyParaProps(look *dxParaLook, p dxParaProps) {
	if p.Align.Val != "" {
		look.align = p.Align.Val
	}
	if p.Spacing != nil {
		if p.Spacing.Before != 0 {
			look.before = float64(p.Spacing.Before) / 15
		}
		if p.Spacing.After != 0 {
			look.after = float64(p.Spacing.After) / 15
		}
		if p.Spacing.Line > 0 {
			switch p.Spacing.Rule {
			case "exact", "atLeast":
				look.lineHeight = fmt.Sprintf("%.1fpx", float64(p.Spacing.Line)/15)
			default:
				look.lineHeight = fmt.Sprintf("%.2f", float64(p.Spacing.Line)/240)
			}
		}
	}
	if p.Indent != nil {
		if p.Indent.Left != 0 {
			look.left = float64(p.Indent.Left) / 15
		}
		if p.Indent.FirstLine != 0 {
			look.first = float64(p.Indent.FirstLine) / 15
		}
		if p.Indent.Hanging != 0 {
			look.hanging = float64(p.Indent.Hanging) / 15
		}
	}
	if p.Shd != nil {
		if c := dxColor(p.Shd.Fill); c != "" {
			look.bg = c
		}
	}
	if p.NumPr != nil {
		if p.NumPr.NumID.Val > 0 {
			look.numID = p.NumPr.NumID.Val
		}
		look.ilvl = p.NumPr.ILvl.Val
	}
	if p.Outline != nil {
		look.outline = p.Outline.Val
		look.outlineSet = true
	}
}

func (r *docxRenderer) paraLook(p dxParaProps) dxParaLook {
	var look dxParaLook
	applyParaProps(&look, r.defaultPara)
	styleID := p.Style.Val
	if styleID == "" {
		styleID = "Normal"
	}
	for _, st := range r.styleChain(styleID) {
		applyParaProps(&look, st.PPr)
	}
	applyParaProps(&look, p)
	return look
}

func (r *docxRenderer) writePara(b *strings.Builder, p *dxPara, inCell bool) {
	look := r.paraLook(p.Props)
	level := 0
	styleID := p.Props.Style.Val
	if styleID == "" {
		styleID = "Normal"
	}
	chain := r.styleChain(styleID)
	for _, st := range chain {
		if n := headingLevel(st); n > 0 {
			level = n
		}
	}
	if look.outlineSet && look.outline >= 0 && look.outline < 6 {
		level = look.outline + 1
	}
	heading := level > 0
	tag := "p"
	if heading {
		tag = fmt.Sprintf("h%d", min(level, 6))
	}

	var attrs []string
	if look.align != "" {
		attrs = append(attrs, "text-align:"+alignment(look.align))
	}
	if look.before > 0 {
		attrs = append(attrs, fmt.Sprintf("margin-top:%.1fpx", look.before))
	}
	if look.after > 0 {
		attrs = append(attrs, fmt.Sprintf("margin-bottom:%.1fpx", look.after))
	}
	if look.lineHeight != "" {
		attrs = append(attrs, "line-height:"+look.lineHeight)
	}
	label := ""
	if look.numID > 0 {
		label = r.listLabel(look.numID, look.ilvl)
	}
	left := look.left
	hanging := look.hanging
	if label != "" && left == 0 && hanging == 0 {
		left = 24 + float64(look.ilvl)*24
		hanging = 18
	}
	if left != 0 {
		attrs = append(attrs, fmt.Sprintf("margin-left:%.1fpx", left))
	}
	if hanging > 0 {
		attrs = append(attrs, fmt.Sprintf("text-indent:-%.1fpx", hanging))
	} else if look.first != 0 {
		attrs = append(attrs, fmt.Sprintf("text-indent:%.1fpx", look.first))
	}
	if look.bg != "" {
		attrs = append(attrs, "background:"+look.bg)
	}
	if inCell {
		attrs = append(attrs, "margin-top:0", "margin-bottom:0")
	}
	fmt.Fprintf(b, "<%s", tag)
	if len(attrs) > 0 {
		fmt.Fprintf(b, ` style="%s"`, strings.Join(attrs, ";"))
	}
	b.WriteString(">")

	if label != "" {
		width := 24.0
		if hanging > 0 {
			width = hanging
		}
		fmt.Fprintf(b, `<span class="li" style="display:inline-block;width:%.1fpx">%s</span>`, width, html.EscapeString(label))
	}

	empty := true
	for _, run := range p.Runs {
		for _, item := range run.Text {
			lk := r.runLook(p.Props, run.Props)
			if heading && !lk.sizeSet {
				lk.size = headingSize(level)
			}
			if item.Text != "" {
				empty = false
				r.writeText(b, item.Text, lk)
			}
			if item.Tab {
				empty = false
				b.WriteString(`<span style="display:inline-block;width:2em"></span>`)
			}
			if item.Break {
				empty = false
				b.WriteString(`<br>`)
			}
			if item.Image != nil {
				empty = false
				r.writeImage(b, item.Image)
			}
		}
	}
	if empty {
		b.WriteString(`<br>`)
	}
	fmt.Fprintf(b, "</%s>", tag)
}

func (r *docxRenderer) writeText(b *strings.Builder, text string, look dxRunLook) {
	var css []string
	if look.bold {
		css = append(css, "font-weight:700")
	}
	if look.italic {
		css = append(css, "font-style:italic")
	}
	if look.underline || look.strike {
		var deco []string
		if look.underline {
			deco = append(deco, "underline")
		}
		if look.strike {
			deco = append(deco, "line-through")
		}
		css = append(css, "text-decoration:"+strings.Join(deco, " "))
	}
	if look.color != "" {
		css = append(css, "color:"+look.color)
	}
	if look.bg != "" {
		css = append(css, "background-color:"+look.bg)
	}
	if look.sizeSet {
		css = append(css, fmt.Sprintf("font-size:%.2fpx", look.size))
	}
	if look.font != "" && look.font != r.defaultFont {
		css = append(css, "font-family:"+look.font)
	}
	if look.vert != "" {
		css = append(css, "vertical-align:"+look.vert+";font-size:.75em")
	}
	escaped := html.EscapeString(text)
	if strings.Contains(escaped, "\n") {
		escaped = strings.ReplaceAll(escaped, "\n", "<br>")
	}
	if len(css) == 0 {
		b.WriteString(escaped)
		return
	}
	fmt.Fprintf(b, `<span style="%s">%s</span>`, strings.Join(css, ";"), escaped)
}

func (r *docxRenderer) writeImage(b *strings.Builder, img *dxImage) {
	rel, ok := r.rels[img.Rel]
	if !ok {
		return
	}
	target := strings.TrimPrefix(rel, "/")
	if !strings.HasPrefix(strings.ToLower(target), "word/") {
		target = path.Clean(path.Join("word", target))
	}
	data, mime := r.imageData(target)
	if data == "" {
		return
	}
	w := float64(img.Width) / 9525
	h := float64(img.Height) / 9525
	if w <= 0 || h <= 0 {
		return
	}
	if w > r.contentWidth {
		h *= r.contentWidth / w
		w = r.contentWidth
	}
	fmt.Fprintf(b, `<img src="data:%s;base64,%s" style="width:%.1fpx;height:%.1fpx" alt="">`, mime, data, w, h)
}

func (r *docxRenderer) imageData(name string) (string, string) {
	ext := strings.ToLower(path.Ext(name))
	var mime string
	switch ext {
	case ".png":
		mime = "image/png"
	case ".jpg", ".jpeg":
		mime = "image/jpeg"
	case ".gif":
		mime = "image/gif"
	case ".bmp":
		mime = "image/bmp"
	case ".webp":
		mime = "image/webp"
	case ".svg":
		mime = "image/svg+xml"
	default:
		return "", ""
	}
	data := r.read(name)
	if len(data) == 0 || len(data) > 12<<20 || int64(len(data)) > r.imageBudget {
		return "", ""
	}
	r.imageBudget -= int64(len(data))
	return base64.StdEncoding.EncodeToString(data), mime
}

func (r *docxRenderer) writeTable(b *strings.Builder, tbl *dxTable, inCell bool) {
	if len(tbl.Cols) == 0 && len(tbl.Rows) > 0 {
		// The table planner keeps grid columns in Cols; a table without a
		// grid uses equal columns.
	}
	bordered := tableBordered(tbl.Props.Borders)
	class := "dx"
	if bordered {
		class += " bordered"
	}
	var widths []float64
	var total float64
	for _, c := range tbl.Cols {
		if c.W > 0 {
			widths = append(widths, float64(c.W))
			total += float64(c.W)
		}
	}
	fmt.Fprintf(b, `<table class="%s"`, class)
	if total > 0 && len(widths) == len(tbl.Cols) {
		var css []string
		for _, w := range widths {
			css = append(css, fmt.Sprintf("%.1f%%", w/total*100))
		}
		b.WriteString(`><colgroup>`)
		for _, w := range css {
			fmt.Fprintf(b, `<col style="width:%s">`, w)
		}
		b.WriteString(`</colgroup>`)
	} else {
		b.WriteString(`>`)
	}
	for _, row := range tbl.Rows {
		b.WriteString(`<tr`)
		if row.Height != nil && row.Height.Val > 0 {
			fmt.Fprintf(b, ` style="height:%.1fpx"`, float64(row.Height.Val)/20*96/72)
		}
		b.WriteString(`>`)
		for _, cell := range row.Cells {
			b.WriteString(`<td`)
			if cell.Props.Span != nil && cell.Props.Span.Val > 1 {
				fmt.Fprintf(b, ` colspan="%d"`, cell.Props.Span.Val)
			}
			var css []string
			if cell.Props.Shd != nil {
				if c := dxColor(cell.Props.Shd.Fill); c != "" {
					css = append(css, "background:"+c)
				}
			}
			if v := strings.ToLower(cell.Props.VAlign.Val); v == "center" || v == "bottom" {
				css = append(css, "vertical-align:"+v)
			}
			if cell.Props.Borders != nil {
				css = append(css, borderCSS(cell.Props.Borders)...)
			}
			if len(css) > 0 {
				fmt.Fprintf(b, ` style="%s"`, strings.Join(css, ";"))
			}
			b.WriteString(`>`)
			if len(cell.Blocks) == 0 {
				b.WriteString(`<p><br></p>`)
			}
			for i := range cell.Blocks {
				r.writeBlock(b, &cell.Blocks[i], true)
			}
			b.WriteString(`</td>`)
		}
		b.WriteString(`</tr>`)
	}
	b.WriteString(`</table>`)
}

func tableBordered(b *dxBorders) bool {
	if b == nil {
		return false
	}
	for _, side := range []*dxBorder{b.Top, b.Bottom, b.Left, b.Right, b.InsideH, b.InsideV} {
		if side != nil && side.Val != "" && !strings.EqualFold(side.Val, "nil") && !strings.EqualFold(side.Val, "none") {
			return true
		}
	}
	return false
}

func borderCSS(b *dxBorders) []string {
	var css []string
	add := func(side string, bd *dxBorder) {
		if bd == nil || bd.Val == "" || strings.EqualFold(bd.Val, "nil") || strings.EqualFold(bd.Val, "none") {
			return
		}
		px := 1.0
		if bd.Size > 0 {
			px = max(1, float64(bd.Size)/8)
		}
		color := dxColor(bd.Color)
		if color == "" {
			color = "#808080"
		}
		css = append(css, fmt.Sprintf("border-%s:%.1fpx solid %s", side, px, color))
	}
	add("top", b.Top)
	add("bottom", b.Bottom)
	add("left", b.Left)
	add("right", b.Right)
	return css
}

func (r *docxRenderer) listLabel(numID, ilvl int) string {
	absID, ok := r.numAbs[numID]
	if !ok {
		return ""
	}
	levels := r.abstract[absID]
	lvl := levels[ilvl]
	if lvl == nil {
		return ""
	}
	key := [2]int{numID, ilvl}
	start := 1
	if lvl.Start != nil && lvl.Start.Val > 0 {
		start = lvl.Start.Val
	}
	r.counters[key]++
	if r.counters[key] < start {
		r.counters[key] = start
	}
	for l := ilvl + 1; l < 9; l++ {
		delete(r.counters, [2]int{numID, l})
	}
	format := strings.ToLower(lvl.NumFmt.Val)
	if format == "bullet" || format == "none" {
		switch ilvl {
		case 0:
			return "•"
		case 1:
			return "◦"
		default:
			return "▪"
		}
	}
	text := lvl.Text.Val
	if text == "" {
		text = "%1."
	}
	for i := 0; i < 9; i++ {
		placeholder := fmt.Sprintf("%%%d", i+1)
		if !strings.Contains(text, placeholder) {
			continue
		}
		value := 0
		if i == ilvl {
			value = r.counters[key]
		} else {
			value = r.counters[[2]int{numID, i}]
			if value == 0 {
				if l := levels[i]; l != nil && l.Start != nil {
					value = l.Start.Val
				} else {
					value = 1
				}
			}
		}
		levelFormat := format
		if l := levels[i]; l != nil && l.NumFmt.Val != "" {
			levelFormat = strings.ToLower(l.NumFmt.Val)
		}
		text = strings.ReplaceAll(text, placeholder, formatNumber(value, levelFormat))
	}
	return text
}

func headingLevel(st *dxStyle) int {
	name := strings.ToLower(strings.TrimSpace(st.Name.Val))
	if strings.HasPrefix(name, "heading ") {
		if n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(name, "heading "))); err == nil && n >= 1 && n <= 6 {
			return n
		}
	}
	id := strings.ToLower(st.ID)
	if strings.HasPrefix(id, "heading") {
		if n, err := strconv.Atoi(strings.TrimPrefix(id, "heading")); err == nil && n >= 1 && n <= 6 {
			return n
		}
	}
	if name == "title" {
		return 1
	}
	return 0
}

func headingSize(level int) float64 {
	pt := []float64{24, 20, 16, 14, 12, 11}
	if level < 1 || level > len(pt) {
		return 14 * 96 / 72
	}
	return pt[level-1] * 96 / 72
}

func atoiOr(s string, fallback int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return n
	}
	return fallback
}

func toggleValue(v string) bool {
	switch strings.ToLower(v) {
	case "0", "false", "off", "none":
		return false
	}
	return true
}

func alignment(v string) string {
	switch v {
	case "center":
		return "center"
	case "right", "end":
		return "right"
	case "both", "distribute":
		return "justify"
	default:
		return "left"
	}
}

func dxColor(c string) string {
	c = strings.TrimSpace(c)
	if c == "" || strings.EqualFold(c, "auto") {
		return ""
	}
	c = strings.TrimPrefix(c, "#")
	if len(c) == 8 {
		c = c[2:]
	}
	if len(c) != 6 {
		return ""
	}
	for _, r := range c {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return ""
		}
	}
	return "#" + c
}

func highlightColor(v string) string {
	switch strings.ToLower(v) {
	case "yellow":
		return "#fff59d"
	case "green":
		return "#c8e6c9"
	case "cyan":
		return "#b2ebf2"
	case "magenta":
		return "#f8bbd0"
	case "blue":
		return "#bbdefb"
	case "red":
		return "#ffcdd2"
	case "darkblue":
		return "#90caf9"
	case "darkcyan":
		return "#80deea"
	case "darkgreen":
		return "#a5d6a7"
	case "darkmagenta":
		return "#f48fb1"
	case "darkred":
		return "#ef9a9a"
	case "darkyellow":
		return "#ffe082"
	case "gray":
		return "#e0e0e0"
	case "lightgray":
		return "#eeeeee"
	case "black":
		return "#000000"
	case "white":
		return "#ffffff"
	}
	return ""
}

func firstFont(f dxFonts) string {
	for _, name := range []string{f.EastAsia, f.Ascii, f.HAnsi} {
		if name != "" {
			return name
		}
	}
	return ""
}

func fontStack(name string) string {
	name = strings.Trim(name, "\"")
	return `"` + strings.ReplaceAll(name, `"`, "") + `","Segoe UI","Microsoft YaHei",sans-serif`
}

func formatNumber(v int, format string) string {
	if v < 1 {
		v = 1
	}
	switch format {
	case "lowerLetter":
		return letters(v, false)
	case "upperLetter":
		return letters(v, true)
	case "lowerRoman":
		return roman(v, false)
	case "upperRoman":
		return roman(v, true)
	case "decimalZero":
		return fmt.Sprintf("%02d", v)
	default:
		return strconv.Itoa(v)
	}
}

func letters(v int, upper bool) string {
	var b strings.Builder
	for v > 0 {
		v--
		ch := byte('a' + v%26)
		if upper {
			ch = byte('A' + v%26)
		}
		b.WriteByte(ch)
		v /= 26
	}
	out := b.String()
	runes := []rune(out)
	slices.Reverse(runes)
	return string(runes)
}

func roman(v int, upper bool) string {
	vals := []struct {
		n int
		s string
	}{{1000, "m"}, {900, "cm"}, {500, "d"}, {400, "cd"}, {100, "c"}, {90, "xc"}, {50, "l"}, {40, "xl"}, {10, "x"}, {9, "ix"}, {5, "v"}, {4, "iv"}, {1, "i"}}
	var b strings.Builder
	for _, item := range vals {
		for v >= item.n {
			b.WriteString(item.s)
			v -= item.n
		}
	}
	if upper {
		return strings.ToUpper(b.String())
	}
	return b.String()
}
