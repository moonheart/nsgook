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
	"sort"
	"strconv"
	"strings"
)

// ---------- PresentationML model ----------

type pxColorChoice struct {
	Srgb *struct {
		Val string `xml:"val,attr"`
	} `xml:"srgbClr"`
	Scheme *struct {
		Val string `xml:"val,attr"`
	} `xml:"schemeClr"`
	Prst *struct {
		Val string `xml:"val,attr"`
	} `xml:"prstClr"`
	Sys *struct {
		Last string `xml:"lastClr,attr"`
	} `xml:"sysClr"`
}

type pxColorRef struct {
	Val   string    `xml:"val,attr"`
	Alpha *pxIntVal `xml:"alpha"`
}

type pxFill struct {
	Srgb   *pxColorRef `xml:"srgbClr"`
	Scheme *pxColorRef `xml:"schemeClr"`
	Prst   *pxColorRef `xml:"prstClr"`
}

type pxXfrm struct {
	Off struct {
		X int64 `xml:"x,attr"`
		Y int64 `xml:"y,attr"`
	} `xml:"off"`
	Ext struct {
		Cx int64 `xml:"cx,attr"`
		Cy int64 `xml:"cy,attr"`
	} `xml:"ext"`
}

func (x *pxXfrm) empty() bool {
	return x == nil || (x.Ext.Cx <= 0 && x.Ext.Cy <= 0)
}

func (x *pxXfrm) px() (x0, y0, w, h float64) {
	if x == nil {
		return 0, 0, 0, 0
	}
	return float64(x.Off.X) / 9525, float64(x.Off.Y) / 9525, float64(x.Ext.Cx) / 9525, float64(x.Ext.Cy) / 9525
}

type pxLine struct {
	Width int64     `xml:"w,attr"`
	Fill  *pxFill   `xml:"solidFill"`
	No    *struct{} `xml:"noFill"`
}

type pxShapeProps struct {
	Xfrm *pxXfrm   `xml:"xfrm"`
	Fill *pxFill   `xml:"solidFill"`
	No   *struct{} `xml:"noFill"`
	Line *pxLine   `xml:"ln"`
}

type pxPlaceholder struct {
	Type string `xml:"type,attr"`
	Idx  string `xml:"idx,attr"`
}

type pxShape struct {
	Placeholder *pxPlaceholder `xml:"nvSpPr>nvPr>ph"`
	Props       pxShapeProps   `xml:"spPr"`
	Tx          *pxTxBody      `xml:"txBody"`
}

type pxPic struct {
	Props pxShapeProps `xml:"spPr"`
	Blip  *struct {
		Embed string `xml:"embed,attr"`
		Link  string `xml:"link,attr"`
	} `xml:"blipFill>blip"`
}

type pxGraphicFrame struct {
	Xfrm  *pxXfrm  `xml:"xfrm"`
	Table *pxTable `xml:"graphic>graphicData>tbl"`
}

type pxTable struct {
	Grid []struct {
		W int64 `xml:"w,attr"`
	} `xml:"tblGrid>gridCol"`
	Rows []pxTableRow `xml:"tr"`
}

type pxTableRow struct {
	Height int64         `xml:"h,attr"`
	Cells  []pxTableCell `xml:"tc"`
}

type pxTableCell struct {
	Body pxTxBody `xml:"txBody"`
}

type pxBodyPr struct {
	Anchor string `xml:"anchor,attr"`
	LIns   *int64 `xml:"lIns,attr"`
	RIns   *int64 `xml:"rIns,attr"`
	TIns   *int64 `xml:"tIns,attr"`
	BIns   *int64 `xml:"bIns,attr"`
}

type pxTxBody struct {
	Body  pxBodyPr `xml:"bodyPr"`
	Paras []pxPara `xml:"p"`
}

type pxPara struct {
	Props pxParaProps
	Items []pxInline
}

type pxInline struct {
	Run   *pxRun
	Break bool
}

type pxRun struct {
	Props pxRunProps `xml:"rPr"`
	Text  string     `xml:"t"`
}

type pxParaProps struct {
	Align  string `xml:"algn,attr"`
	Bullet *struct {
		Char string `xml:"char,attr"`
	} `xml:"buChar"`
	AutoNum *struct {
		Type string `xml:"type,attr"`
	} `xml:"buAutoNum"`
	NoBullet *struct{}  `xml:"buNone"`
	LnSpc    *pxSpacing `xml:"lnSpc"`
	SpcBef   *pxSpacing `xml:"spcBef"`
	SpcAft   *pxSpacing `xml:"spcAft"`
	MarL     int64      `xml:"marL,attr"`
	Indent   int64      `xml:"indent,attr"`
}

type pxSpacing struct {
	Pct *pxIntVal `xml:"spcPct"`
	Pts *pxIntVal `xml:"spcPts"`
}

type pxIntVal struct {
	Val int `xml:"val,attr"`
}

type pxRunProps struct {
	Sz     int     `xml:"sz,attr"` // hundredths of a point
	Bold   bool    `xml:"b,attr"`
	Italic bool    `xml:"i,attr"`
	Under  string  `xml:"u,attr"`
	Strike string  `xml:"strike,attr"`
	Fill   *pxFill `xml:"solidFill"`
	Font   *struct {
		Typeface string `xml:"typeface,attr"`
	} `xml:"latin"`
}

func (p *pxPara) UnmarshalXML(dec *xml.Decoder, start xml.StartElement) error {
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
				run := &pxRun{}
				if err := dec.DecodeElement(run, &t); err != nil {
					return err
				}
				p.Items = append(p.Items, pxInline{Run: run})
			case "br":
				p.Items = append(p.Items, pxInline{Break: true})
				if err := dec.Skip(); err != nil {
					return err
				}
			case "fld":
				run := &pxRun{}
				if err := dec.DecodeElement(run, &t); err != nil {
					return err
				}
				p.Items = append(p.Items, pxInline{Run: run})
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

type pxSpItem struct {
	Shape *pxShape
	Pic   *pxPic
	Frame *pxGraphicFrame
	Group *pxGroup
}

type pxGroup struct {
	Xfrm  pxGroupXfrm
	Items []pxSpItem
}

type pxGroupXfrm struct {
	Off   struct{ X, Y int64 }   `xml:"off"`
	Ext   struct{ Cx, Cy int64 } `xml:"ext"`
	ChOff struct{ X, Y int64 }   `xml:"chOff"`
	ChExt struct{ Cx, Cy int64 } `xml:"chExt"`
}

type pxSpTree struct {
	Items []pxSpItem
}

func (t *pxSpTree) UnmarshalXML(dec *xml.Decoder, start xml.StartElement) error {
	for {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		switch se := tok.(type) {
		case xml.StartElement:
			item, err := decodeSpItem(dec, se)
			if err != nil {
				return err
			}
			t.Items = append(t.Items, item)
		case xml.EndElement:
			if se.Name == start.Name {
				return nil
			}
		}
	}
}

func decodeSpItem(dec *xml.Decoder, start xml.StartElement) (pxSpItem, error) {
	switch start.Name.Local {
	case "sp":
		s := &pxShape{}
		return pxSpItem{Shape: s}, dec.DecodeElement(s, &start)
	case "pic":
		p := &pxPic{}
		return pxSpItem{Pic: p}, dec.DecodeElement(p, &start)
	case "graphicFrame":
		f := &pxGraphicFrame{}
		return pxSpItem{Frame: f}, dec.DecodeElement(f, &start)
	case "grpSp":
		g := &pxGroup{}
		return pxSpItem{Group: g}, dec.DecodeElement(g, &start)
	}
	return pxSpItem{}, dec.Skip()
}

func (g *pxGroup) UnmarshalXML(dec *xml.Decoder, start xml.StartElement) error {
	for {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		switch se := tok.(type) {
		case xml.StartElement:
			switch se.Name.Local {
			case "grpSpPr":
				var pr struct {
					Xfrm pxGroupXfrm `xml:"xfrm"`
				}
				if err := dec.DecodeElement(&pr, &se); err != nil {
					return err
				}
				g.Xfrm = pr.Xfrm
			default:
				item, err := decodeSpItem(dec, se)
				if err != nil {
					return err
				}
				g.Items = append(g.Items, item)
			}
		case xml.EndElement:
			if se.Name == start.Name {
				return nil
			}
		}
	}
}

type pxLvl1 struct {
	DefRPr pxRunProps `xml:"defRPr"`
	LnSpc  *pxSpacing `xml:"lnSpc"`
	SpcBef *pxSpacing `xml:"spcBef"`
	MarL   int64      `xml:"marL,attr"`
	Indent int64      `xml:"indent,attr"`
	Bullet *struct {
		Char string `xml:"char,attr"`
	} `xml:"buChar"`
	NoBullet *struct{} `xml:"buNone"`
}

type pxTxStyles struct {
	Title pxLvl1 `xml:"titleStyle>lvl1pPr"`
	Body  pxLvl1 `xml:"bodyStyle>lvl1pPr"`
	Other pxLvl1 `xml:"otherStyle>lvl1pPr"`
}

type pxSlideXML struct {
	Bg *struct {
		BgPr *struct {
			Fill *pxFill `xml:"solidFill"`
		} `xml:"bgPr"`
		BgRef *struct {
			Idx int `xml:"idx,attr"`
		} `xml:"bgRef"`
	} `xml:"cSld>bg"`
	Tree     pxSpTree    `xml:"cSld>spTree"`
	TxStyles *pxTxStyles `xml:"txStyles"`
}

type pxPresentationXML struct {
	Size struct {
		Cx int64 `xml:"cx,attr"`
		Cy int64 `xml:"cy,attr"`
	} `xml:"sldSz"`
	Slides []struct {
		RID string `xml:"id,attr"`
	} `xml:"sldIdLst>sldId"`
}

type pxThemeXML struct {
	Elements struct {
		Scheme struct {
			Dk1     pxColorChoice `xml:"dk1"`
			Lt1     pxColorChoice `xml:"lt1"`
			Dk2     pxColorChoice `xml:"dk2"`
			Lt2     pxColorChoice `xml:"lt2"`
			Accent1 pxColorChoice `xml:"accent1"`
			Accent2 pxColorChoice `xml:"accent2"`
			Accent3 pxColorChoice `xml:"accent3"`
			Accent4 pxColorChoice `xml:"accent4"`
			Accent5 pxColorChoice `xml:"accent5"`
			Accent6 pxColorChoice `xml:"accent6"`
		} `xml:"clrScheme"`
		Fonts struct {
			Major struct {
				Latin struct {
					Typeface string `xml:"typeface,attr"`
				} `xml:"latin"`
			} `xml:"majorFont"`
			Minor struct {
				Latin struct {
					Typeface string `xml:"typeface,attr"`
				} `xml:"latin"`
			} `xml:"minorFont"`
		} `xml:"fontScheme"`
	} `xml:"themeElements"`
}

// ---------- PresentationML rendering ----------

type pptxRenderer struct {
	files       map[string]*zip.File
	theme       pxThemeXML
	colors      map[string]string
	major       string
	minor       string
	slideW      float64
	slideH      float64
	rels        map[string]map[string]string // slide path -> rels
	layouts     map[string]*pxSlideXML
	masters     map[string]*pxSlideXML
	imageBudget int64
}

func renderPPTX(file string) (string, error) {
	zr, err := zip.OpenReader(file)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	r := &pptxRenderer{files: map[string]*zip.File{}, colors: map[string]string{}, rels: map[string]map[string]string{}, layouts: map[string]*pxSlideXML{}, masters: map[string]*pxSlideXML{}, imageBudget: 24 << 20}
	for _, f := range zr.File {
		r.files[strings.ToLower(f.Name)] = f
	}
	if b := r.read("ppt/presentation.xml"); len(b) > 0 {
		var pres pxPresentationXML
		_ = xml.Unmarshal(b, &pres)
		if pres.Size.Cx > 0 && pres.Size.Cy > 0 {
			r.slideW = float64(pres.Size.Cx) / 9525
			r.slideH = float64(pres.Size.Cy) / 9525
		}
	}
	if r.slideW <= 0 {
		r.slideW, r.slideH = 1280, 720
	}

	r.loadTheme()
	var presRels map[string]string
	if b := r.read("ppt/_rels/presentation.xml.rels"); len(b) > 0 {
		presRels = pxRels(b)
	}
	var slidePaths []string
	if b := r.read("ppt/presentation.xml"); len(b) > 0 {
		var pres pxPresentationXML
		_ = xml.Unmarshal(b, &pres)
		for _, s := range pres.Slides {
			if target, ok := presRels[s.RID]; ok {
				slidePaths = append(slidePaths, path.Clean(path.Join("ppt", target)))
			}
		}
	}
	if len(slidePaths) == 0 {
		for name := range r.files {
			if strings.HasPrefix(name, "ppt/slides/slide") && strings.HasSuffix(name, ".xml") && !strings.Contains(name, "_rels") {
				slidePaths = append(slidePaths, name)
			}
		}
		sort.Slice(slidePaths, func(i, j int) bool { return slideNumber(slidePaths[i]) < slideNumber(slidePaths[j]) })
	}
	if len(slidePaths) == 0 {
		return "", fmt.Errorf("officehtml: %s has no slides", filepath.Base(file))
	}
	if len(slidePaths) > 80 {
		slidePaths = slidePaths[:80]
	}

	var body strings.Builder
	for i, sp := range slidePaths {
		var slide pxSlideXML
		if b := r.read(sp); len(b) > 0 {
			_ = xml.Unmarshal(b, &slide)
		}
		layout := r.layoutFor(sp)
		master := r.masterFor(layout)
		body.WriteString(`<figure class="slide-card">`)
		body.WriteString(r.slideSVG(&slide, sp, layout, master))
		fmt.Fprintf(&body, `<figcaption>%d / %d</figcaption>`, i+1, len(slidePaths))
		body.WriteString(`</figure>`)
	}

	css := `
.stage{max-width:1100px;margin:24px auto 48px}
.slide-card{margin:0 0 26px;background:#fff;border-radius:4px;box-shadow:0 6px 22px rgba(0,0,0,.16);overflow:hidden}
.slide-card svg{display:block;width:100%;height:auto;background:#fff}
.slide-card figcaption{padding:6px 10px 8px;font-size:11px;color:#8a8f98;text-align:center;background:#fff}
.sl-text{font-family:"Segoe UI","Microsoft YaHei",sans-serif;color:#222;overflow:hidden;word-wrap:break-word;line-height:1.15}
.sl-text p{margin:0;padding:0}
.sl-table{border-collapse:collapse;width:100%;height:100%;table-layout:fixed}
.sl-table td{border:1px solid #cfcfcf;padding:3px 5px;vertical-align:top;color:#222;font-size:12px;overflow:hidden}
@media(max-width:760px){.stage{margin:10px auto 30px}.slide-card{border-radius:0}}
`
	return document(filepath.Base(file), css, `<div class="stage">`+body.String()+`</div>`), nil
}

func (r *pptxRenderer) loadTheme() {
	// The theme a deck uses is normally the first one; a master can point
	// elsewhere, which this preview approximates.
	if b := r.read("ppt/theme/theme1.xml"); len(b) > 0 {
		_ = xml.Unmarshal(b, &r.theme)
	}
	scheme := r.theme.Elements.Scheme
	r.colors = map[string]string{
		"bg1": pxChoiceColor(scheme.Lt1), "tx1": pxChoiceColor(scheme.Dk1),
		"bg2": pxChoiceColor(scheme.Lt2), "tx2": pxChoiceColor(scheme.Dk2),
		"accent1": pxChoiceColor(scheme.Accent1), "accent2": pxChoiceColor(scheme.Accent2),
		"accent3": pxChoiceColor(scheme.Accent3), "accent4": pxChoiceColor(scheme.Accent4),
		"accent5": pxChoiceColor(scheme.Accent5), "accent6": pxChoiceColor(scheme.Accent6),
	}
	r.major = r.theme.Elements.Fonts.Major.Latin.Typeface
	r.minor = r.theme.Elements.Fonts.Minor.Latin.Typeface
}

func (r *pptxRenderer) read(name string) []byte {
	f := r.files[strings.ToLower(name)]
	if f == nil {
		return nil
	}
	rc, err := f.Open()
	if err != nil {
		return nil
	}
	defer rc.Close()
	b, _ := io.ReadAll(io.LimitReader(rc, 16<<20))
	return b
}

func (r *pptxRenderer) layoutFor(slide string) *pxSlideXML {
	rels := r.slideRels(slide)
	for _, target := range rels {
		if strings.Contains(strings.ToLower(target), "slidelayout") {
			lp := path.Clean(path.Join(path.Dir(slide), target))
			if tree := r.placeholders(lp); tree != nil {
				return tree
			}
		}
	}
	return nil
}

func (r *pptxRenderer) masterFor(layout *pxSlideXML) *pxSlideXML {
	if layout == nil {
		return nil
	}
	// Layouts are keyed by path, so find the one whose tree is layout.
	for p, t := range r.layouts {
		if t != layout {
			continue
		}
		for _, target := range r.slideRels(p) {
			if strings.Contains(strings.ToLower(target), "slidemaster") {
				mp := path.Clean(path.Join(path.Dir(p), target))
				return r.placeholders(mp)
			}
		}
	}
	return nil
}

func (r *pptxRenderer) slideRels(file string) map[string]string {
	if rels, ok := r.rels[file]; ok {
		return rels
	}
	relPath := path.Join(path.Dir(file), "_rels", path.Base(file)+".rels")
	rels := map[string]string{}
	if b := r.read(relPath); len(b) > 0 {
		rels = pxRels(b)
	}
	r.rels[file] = rels
	return rels
}

func (r *pptxRenderer) placeholders(file string) *pxSlideXML {
	if t, ok := r.layouts[file]; ok {
		return t
	}
	var slide pxSlideXML
	if b := r.read(file); len(b) > 0 {
		_ = xml.Unmarshal(b, &slide)
	} else {
		return nil
	}
	r.layouts[file] = &slide
	return &slide
}

func (r *pptxRenderer) slideSVG(slide *pxSlideXML, slidePath string, layout, master *pxSlideXML) string {
	var b strings.Builder
	W, H := r.slideW, r.slideH
	fmt.Fprintf(&b, `<svg viewBox="0 0 %.0f %.0f" xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink">`, W, H)
	bg := r.backgroundColor(slide, layout, master)
	fmt.Fprintf(&b, `<rect width="%.0f" height="%.0f" fill="%s"/>`, W, H, bg)

	rels := r.slideRels(slidePath)
	for i := range slide.Tree.Items {
		item := &slide.Tree.Items[i]
		switch {
		case item.Shape != nil:
			r.writeShape(&b, item.Shape, layout, master)
		case item.Pic != nil:
			r.writePicture(&b, item.Pic, rels)
		case item.Frame != nil:
			r.writeFrame(&b, item.Frame)
		case item.Group != nil:
			r.writeGroup(&b, item.Group, layout, master, rels)
		}
	}
	b.WriteString(`</svg>`)
	return b.String()
}

func (r *pptxRenderer) writeShape(b *strings.Builder, s *pxShape, layout, master *pxSlideXML) {
	xf := s.Props.Xfrm
	if xf.empty() {
		if layout != nil {
			xf = placeholderXfrm(s.Placeholder, &layout.Tree)
		}
		if xf.empty() && master != nil {
			xf = placeholderXfrm(s.Placeholder, &master.Tree)
		}
	}
	if xf == nil || xf.Ext.Cx <= 0 || xf.Ext.Cy <= 0 {
		return
	}
	x, y, w, h := xf.px()
	fill := ""
	if s.Props.Fill != nil {
		fill = r.fillColor(s.Props.Fill)
	}
	if fill != "" {
		alpha := r.fillAlpha(s.Props.Fill)
		if alpha < 1 {
			fmt.Fprintf(b, `<rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" fill="%s" fill-opacity="%.3f"/>`, x, y, w, h, fill, alpha)
		} else {
			fmt.Fprintf(b, `<rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" fill="%s"/>`, x, y, w, h, fill)
		}
	}
	if s.Props.Line != nil && s.Props.Line.Fill != nil && s.Props.Line.No == nil {
		stroke := r.fillColor(s.Props.Line.Fill)
		width := float64(s.Props.Line.Width) / 9525
		if width <= 0 {
			width = 1
		}
		if stroke != "" {
			fmt.Fprintf(b, `<rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" fill="none" stroke="%s" stroke-width="%.2f"/>`, x, y, w, h, stroke, width)
		}
	}
	if s.Tx == nil || len(s.Tx.Paras) == 0 {
		return
	}
	r.writeText(b, s, x, y, w, h, master)
}

func (r *pptxRenderer) writeText(b *strings.Builder, s *pxShape, x, y, w, h float64, master *pxSlideXML) {
	insL, insR, insT, insB := 9.6, 9.6, 4.8, 4.8
	if s.Tx.Body.LIns != nil {
		insL = float64(*s.Tx.Body.LIns) / 9525
	}
	if s.Tx.Body.RIns != nil {
		insR = float64(*s.Tx.Body.RIns) / 9525
	}
	if s.Tx.Body.TIns != nil {
		insT = float64(*s.Tx.Body.TIns) / 9525
	}
	if s.Tx.Body.BIns != nil {
		insB = float64(*s.Tx.Body.BIns) / 9525
	}
	justify := "flex-start"
	switch s.Tx.Body.Anchor {
	case "ctr":
		justify = "center"
	case "b":
		justify = "flex-end"
	}
	type defaultLook struct {
		size   float64
		bold   bool
		line   float64
		before float64
		bullet string
	}
	def := defaultLook{size: 18 * 96 / 72}
	defFont := r.minor
	defColor := ""
	if s.Placeholder != nil {
		switch s.Placeholder.Type {
		case "title", "ctrTitle":
			def.size, def.bold = 40*96/72, true
			defFont = r.major
		case "subTitle":
			def.size = 24 * 96 / 72
		case "body", "obj":
			def.size = 20 * 96 / 72
		}
	}
	if master != nil && master.TxStyles != nil {
		style := &master.TxStyles.Other
		if s.Placeholder != nil {
			switch s.Placeholder.Type {
			case "title", "ctrTitle":
				style = &master.TxStyles.Title
			case "subTitle", "body", "obj":
				style = &master.TxStyles.Body
			}
		}
		def.bold = style.DefRPr.Bold
		if style.DefRPr.Sz > 0 {
			def.size = float64(style.DefRPr.Sz) / 100 * 96 / 72
		}
		if style.DefRPr.Fill != nil {
			defColor = r.fillColor(style.DefRPr.Fill)
		}
		if style.DefRPr.Font != nil && style.DefRPr.Font.Typeface != "" {
			defFont = themeTypeface(r, style.DefRPr.Font.Typeface)
		}
		if style.LnSpc != nil && style.LnSpc.Pct != nil {
			def.line = float64(style.LnSpc.Pct.Val) / 100000
		}
		if style.SpcBef != nil && style.SpcBef.Pts != nil {
			def.before = float64(style.SpcBef.Pts.Val) / 100 * 96 / 72
		}
		if style.Bullet != nil && style.NoBullet == nil {
			def.bullet = style.Bullet.Char
		}
	}
	fmt.Fprintf(b, `<foreignObject x="%.2f" y="%.2f" width="%.2f" height="%.2f">`, x, y, w, h)
	fmt.Fprintf(b, `<div xmlns="http://www.w3.org/1999/xhtml" class="sl-text" style="width:100%%;height:100%%;display:flex;flex-direction:column;justify-content:%s;padding:%.2fpx %.2fpx %.2fpx %.2fpx">`, justify, insT, insR, insB, insL)

	num := 0
	for _, p := range s.Tx.Paras {
		var style []string
		align := "left"
		switch p.Props.Align {
		case "ctr":
			align = "center"
		case "r":
			align = "right"
		case "just":
			align = "justify"
		}
		style = append(style, "text-align:"+align)
		if def.line > 0 {
			style = append(style, fmt.Sprintf("line-height:%.2f", def.line))
		}
		if def.before > 0 {
			style = append(style, fmt.Sprintf("margin-top:%.2fpx", def.before))
		}
		if p.Props.LnSpc != nil {
			if p.Props.LnSpc.Pct != nil {
				style = append(style, fmt.Sprintf("line-height:%.2f", float64(p.Props.LnSpc.Pct.Val)/100000))
			} else if p.Props.LnSpc.Pts != nil {
				style = append(style, fmt.Sprintf("line-height:%.2fpx", float64(p.Props.LnSpc.Pts.Val)/100*96/72))
			}
		}
		if p.Props.MarL > 0 || p.Props.Indent != 0 {
			style = append(style, fmt.Sprintf("margin-left:%.2fpx;text-indent:%.2fpx", float64(p.Props.MarL)/9525, float64(p.Props.Indent)/9525))
		}
		if p.Props.SpcBef != nil && p.Props.SpcBef.Pts != nil {
			style = append(style, fmt.Sprintf("margin-top:%.2fpx", float64(p.Props.SpcBef.Pts.Val)/100*96/72))
		}
		if p.Props.SpcAft != nil && p.Props.SpcAft.Pts != nil {
			style = append(style, fmt.Sprintf("margin-bottom:%.2fpx", float64(p.Props.SpcAft.Pts.Val)/100*96/72))
		}
		prefix := ""
		if p.Props.NoBullet == nil {
			switch {
			case p.Props.Bullet != nil:
				prefix = p.Props.Bullet.Char
			case p.Props.AutoNum != nil:
				num++
				prefix = strconv.Itoa(num) + "."
			case def.bullet != "" && s.Placeholder != nil:
				prefix = def.bullet
			}
		}
		fmt.Fprintf(b, `<p style="%s">`, strings.Join(style, ";"))
		if prefix != "" {
			fmt.Fprintf(b, `<span>%s&nbsp;&nbsp;</span>`, html.EscapeString(prefix))
		}
		empty := true
		for _, item := range p.Items {
			if item.Break {
				b.WriteString(`<br>`)
				empty = false
				continue
			}
			if item.Run == nil {
				continue
			}
			text := strings.TrimRight(item.Run.Text, "\r\n")
			if text == "" {
				continue
			}
			empty = false
			r.writeRun(b, item.Run, def.size, def.bold, defFont, defColor)
		}
		if empty {
			b.WriteString(`<br>`)
		}
		b.WriteString(`</p>`)
	}
	b.WriteString(`</div></foreignObject>`)
}

func (r *pptxRenderer) writeRun(b *strings.Builder, run *pxRun, defaultSize float64, defaultBold bool, defaultFont, defaultColor string) {
	rp := run.Props
	size := defaultSize
	if rp.Sz > 0 {
		size = float64(rp.Sz) / 100 * 96 / 72
	}
	var css []string
	css = append(css, fmt.Sprintf("font-size:%.2fpx", size))
	if rp.Bold || defaultBold {
		css = append(css, "font-weight:700")
	}
	if rp.Italic {
		css = append(css, "font-style:italic")
	}
	if rp.Under != "" && !strings.EqualFold(rp.Under, "none") {
		css = append(css, "text-decoration:underline")
	}
	if rp.Strike != "" && !strings.EqualFold(rp.Strike, "noStrike") {
		css = append(css, "text-decoration:line-through")
	}
	color := ""
	if rp.Fill != nil {
		color = r.fillColor(rp.Fill)
	}
	if color == "" {
		color = defaultColor
	}
	if color == "" {
		color = r.colors["tx1"]
	}
	if color == "" {
		color = "#222"
	}
	css = append(css, "color:"+color)
	if rp.Font != nil && rp.Font.Typeface != "" {
		css = append(css, "font-family:"+fontStack(rp.Font.Typeface))
	} else if defaultFont != "" {
		css = append(css, "font-family:"+fontStack(defaultFont))
	}
	fmt.Fprintf(b, `<span style="%s">%s</span>`, strings.Join(css, ";"), strings.ReplaceAll(html.EscapeString(run.Text), "\n", "<br>"))
}

func (r *pptxRenderer) writePicture(b *strings.Builder, pic *pxPic, rels map[string]string) {
	if pic.Props.Xfrm == nil || pic.Blip == nil || pic.Blip.Embed == "" {
		return
	}
	x, y, w, h := pic.Props.Xfrm.px()
	target, ok := rels[pic.Blip.Embed]
	if !ok || w <= 0 || h <= 0 {
		return
	}
	img := r.imageData(pic, target)
	if img == "" {
		return
	}
	fmt.Fprintf(b, `<image x="%.2f" y="%.2f" width="%.2f" height="%.2f" preserveAspectRatio="none" href="%s"/>`, x, y, w, h, img)
}

func (r *pptxRenderer) imageData(pic *pxPic, target string) string {
	_ = pic
	p := path.Clean(path.Join("ppt/slides", target))
	if !strings.HasPrefix(p, "ppt/") {
		p = target
	}
	ext := strings.ToLower(path.Ext(p))
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
		return ""
	}
	data := r.read(p)
	if len(data) == 0 || len(data) > 12<<20 || int64(len(data)) > r.imageBudget {
		return ""
	}
	r.imageBudget -= int64(len(data))
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func (r *pptxRenderer) writeFrame(b *strings.Builder, f *pxGraphicFrame) {
	if f.Table == nil || f.Xfrm == nil {
		return
	}
	x, y, w, h := f.Xfrm.px()
	if w <= 0 || h <= 0 {
		return
	}
	fmt.Fprintf(b, `<foreignObject x="%.2f" y="%.2f" width="%.2f" height="%.2f">`, x, y, w, h)
	b.WriteString(`<table xmlns="http://www.w3.org/1999/xhtml" class="sl-table">`)
	for _, row := range f.Table.Rows {
		b.WriteString(`<tr>`)
		for _, cell := range row.Cells {
			b.WriteString(`<td>`)
			for _, p := range cell.Body.Paras {
				b.WriteString(`<p>`)
				for _, run := range p.Items {
					if run.Run != nil {
						r.writeRun(b, run.Run, 12, false, r.minor, "")
					}
				}
				b.WriteString(`</p>`)
			}
			b.WriteString(`</td>`)
		}
		b.WriteString(`</tr>`)
	}
	b.WriteString(`</table></foreignObject>`)
}

func (r *pptxRenderer) fillColor(f *pxFill) string {
	if f == nil {
		return ""
	}
	if f.Srgb != nil && f.Srgb.Val != "" {
		return "#" + strings.TrimPrefix(f.Srgb.Val, "#")
	}
	if f.Scheme != nil {
		return r.colors[f.Scheme.Val]
	}
	if f.Prst != nil {
		if c, ok := presetColors[strings.ToLower(f.Prst.Val)]; ok {
			return c
		}
	}
	return ""
}

func (r *pptxRenderer) fillAlpha(f *pxFill) float64 {
	if f == nil {
		return 1
	}
	var ref *pxColorRef
	switch {
	case f.Srgb != nil:
		ref = f.Srgb
	case f.Scheme != nil:
		ref = f.Scheme
	case f.Prst != nil:
		ref = f.Prst
	}
	if ref == nil || ref.Alpha == nil {
		return 1
	}
	return min(max(float64(ref.Alpha.Val)/100000, 0), 1)
}

func (r *pptxRenderer) backgroundColor(parts ...*pxSlideXML) string {
	for _, part := range parts {
		if part == nil || part.Bg == nil {
			continue
		}
		if part.Bg.BgPr != nil && part.Bg.BgPr.Fill != nil {
			if c := r.fillColor(part.Bg.BgPr.Fill); c != "" {
				return c
			}
		}
		if part.Bg.BgRef != nil {
			switch part.Bg.BgRef.Idx {
			case 1001:
				if c := r.colors["bg1"]; c != "" {
					return c
				}
			case 1002:
				if c := r.colors["bg2"]; c != "" {
					return c
				}
			}
		}
	}
	return "#ffffff"
}

func (r *pptxRenderer) writeGroup(b *strings.Builder, g *pxGroup, layout, master *pxSlideXML, rels map[string]string) {
	x := g.Xfrm
	sx, sy := 1.0, 1.0
	if x.ChExt.Cx > 0 && x.Ext.Cx > 0 {
		sx = float64(x.Ext.Cx) / float64(x.ChExt.Cx)
	}
	if x.ChExt.Cy > 0 && x.Ext.Cy > 0 {
		sy = float64(x.Ext.Cy) / float64(x.ChExt.Cy)
	}
	tx := float64(x.Off.X-x.ChOff.X) * sx / 9525
	ty := float64(x.Off.Y-x.ChOff.Y) * sy / 9525
	if sx != 1 || sy != 1 || tx != 0 || ty != 0 {
		fmt.Fprintf(b, `<g transform="translate(%.2f %.2f) scale(%.4f %.4f)">`, tx, ty, sx, sy)
	}
	for i := range g.Items {
		item := &g.Items[i]
		switch {
		case item.Shape != nil:
			r.writeShape(b, item.Shape, layout, master)
		case item.Pic != nil:
			r.writePicture(b, item.Pic, rels)
		case item.Frame != nil:
			r.writeFrame(b, item.Frame)
		case item.Group != nil:
			r.writeGroup(b, item.Group, layout, master, rels)
		}
	}
	if sx != 1 || sy != 1 || tx != 0 || ty != 0 {
		b.WriteString(`</g>`)
	}
}

func placeholderXfrm(ph *pxPlaceholder, tree *pxSpTree) *pxXfrm {
	if ph == nil || tree == nil {
		return nil
	}
	var byType *pxXfrm
	for i := range tree.Items {
		s := tree.Items[i].Shape
		if s == nil || s.Placeholder == nil {
			continue
		}
		if ph.Idx != "" && s.Placeholder.Idx == ph.Idx && !s.Props.Xfrm.empty() {
			return s.Props.Xfrm
		}
		if s.Placeholder.Type == ph.Type && !s.Props.Xfrm.empty() {
			if ph.Type != "" {
				return s.Props.Xfrm
			}
			byType = s.Props.Xfrm
		}
	}
	return byType
}

func pxRels(b []byte) map[string]string {
	var rels struct {
		Items []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	_ = xml.Unmarshal(b, &rels)
	out := map[string]string{}
	for _, rel := range rels.Items {
		out[rel.ID] = rel.Target
	}
	return out
}

func pxChoiceColor(c pxColorChoice) string {
	if c.Srgb != nil {
		return "#" + strings.TrimPrefix(c.Srgb.Val, "#")
	}
	if c.Sys != nil {
		return "#" + strings.TrimPrefix(c.Sys.Last, "#")
	}
	return ""
}

func themeTypeface(r *pptxRenderer, name string) string {
	if strings.HasPrefix(name, "+mj") {
		if r.major != "" {
			return r.major
		}
		return r.minor
	}
	if strings.HasPrefix(name, "+mn") {
		return r.minor
	}
	return name
}

func slideNumber(name string) int {
	base := strings.TrimSuffix(path.Base(name), path.Ext(name))
	n, _ := strconv.Atoi(strings.TrimPrefix(base, "slide"))
	return n
}

var presetColors = map[string]string{
	"black": "#000000", "white": "#FFFFFF", "red": "#FF0000", "green": "#00B050",
	"blue": "#0070C0", "yellow": "#FFFF00", "orange": "#FFC000", "gray": "#808080",
	"grey": "#808080", "darkGray": "#404040", "lightGray": "#D9D9D9",
}
