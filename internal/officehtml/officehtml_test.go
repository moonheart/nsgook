package officehtml

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="

func writeZip(t *testing.T, name string, files map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for n, body := range files {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRenderDOCX(t *testing.T) {
	doc := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture">
<w:body>
<w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Quarterly Report</w:t></w:r></w:p>
<w:p><w:r><w:rPr><w:b/><w:color w:val="FF0000"/></w:rPr><w:t>Bold red</w:t></w:r><w:r><w:t xml:space="preserve"> and plain &lt;tag&gt;</w:t></w:r></w:p>
<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>First item</w:t></w:r></w:p>
<w:tbl>
<w:tblPr><w:tblBorders><w:top w:val="single"/><w:insideH w:val="single"/></w:tblBorders></w:tblPr>
<w:tblGrid><w:gridCol w:w="3000"/><w:gridCol w:w="6000"/></w:tblGrid>
<w:tr><w:tc><w:p><w:r><w:t>Name</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>Value</w:t></w:r></w:p></w:tc></w:tr>
<w:tr><w:tc><w:p><w:r><w:t>Alpha</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>42</w:t></w:r></w:p></w:tc></w:tr>
</w:tbl>
<w:p><w:r><w:drawing><wp:inline><wp:extent cx="952500" cy="952500"/><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic><pic:blipFill><a:blip r:embed="rIdImg"/></pic:blipFill></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>
<w:sectPr><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr>
</w:body></w:document>`
	styles := `<?xml version="1.0"?><w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Calibri"/><w:sz w:val="22"/></w:rPr></w:rPrDefault></w:docDefaults>
<w:style w:type="paragraph" w:styleId="Normal"><w:name w:val="Normal"/></w:style>
<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:pPr><w:outlineLvl w:val="0"/></w:pPr><w:rPr><w:b/><w:sz w:val="40"/></w:rPr></w:style>
</w:styles>`
	numbering := `<?xml version="1.0"?><w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:abstractNum w:abstractNumId="0"><w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="bullet"/><w:lvlText w:val="•"/></w:lvl></w:abstractNum>
<w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num></w:numbering>`
	rels := `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rIdImg" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/image1.png"/>
</Relationships>`
	files := map[string]string{
		"word/document.xml":            doc,
		"word/styles.xml":              styles,
		"word/numbering.xml":           numbering,
		"word/_rels/document.xml.rels": rels,
	}
	p := writeZip(t, "report.docx", files)
	// Add the image separately so writeZip stays textual.
	addZipFile(t, p, "word/media/image1.png", tinyPNG)

	htmlOut, err := Render(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Quarterly Report", "<h1", "font-weight:700", "#FF0000", "and plain &lt;tag&gt;",
		"•", "First item", ">Name<", ">Alpha<", "data:image/png;base64,", "width:816px",
	} {
		if !strings.Contains(htmlOut, want) {
			t.Fatalf("rendered DOCX lacks %q\n%s", want, htmlOut)
		}
	}
	if strings.Contains(htmlOut, "<script>") {
		t.Fatal("document text was not escaped")
	}
}

func TestRenderXLSX(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()
	f.SetCellValue("Sheet1", "A1", "Name")
	f.SetCellValue("Sheet1", "B1", "Value")
	f.SetCellValue("Sheet1", "A2", "Alpha")
	f.SetCellValue("Sheet1", "B2", 42)
	bold, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFF0000"},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"FFFF00"}},
		Alignment: &excelize.Alignment{Horizontal: "center", WrapText: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.SetCellStyle("Sheet1", "A1", "B1", bold); err != nil {
		t.Fatal(err)
	}
	if err := f.MergeCell("Sheet1", "A3", "B3"); err != nil {
		t.Fatal(err)
	}
	f.SetCellValue("Sheet1", "A3", "Merged note")
	f.SetColWidth("Sheet1", "A", "A", 20)
	if _, err := f.NewSheet("Second"); err != nil {
		t.Fatal(err)
	}
	f.SetCellValue("Second", "A1", "Other sheet")
	path := filepath.Join(t.TempDir(), "book.xlsx")
	if err := f.SaveAs(path); err != nil {
		t.Fatal(err)
	}

	htmlOut, err := Render(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Name", "Alpha", "42", "Merged note", `colspan="2"`, "Other sheet",
		"#FF0000", "#FFFF00", "font-weight:700", ">Sheet1<", ">Second<",
	} {
		if !strings.Contains(htmlOut, want) {
			t.Fatalf("rendered XLSX lacks %q\n%s", want, htmlOut)
		}
	}
}

func TestRenderPPTX(t *testing.T) {
	presentation := `<?xml version="1.0"?><p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
<p:sldSz cx="12192000" cy="6858000"/><p:sldIdLst><p:sldId id="256" r:id="rId1"/></p:sldIdLst></p:presentation>`
	presRels := `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide1.xml"/></Relationships>`
	slide := `<?xml version="1.0"?><p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
<p:cSld><p:spTree>
<p:sp><p:nvSpPr><p:cNvPr id="1" name="Title"/><p:cNvSpPr/><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:spPr/>
<p:txBody><a:bodyPr anchor="ctr"/><a:p><a:r><a:rPr sz="4000" b="1"/><a:t>Deck title</a:t></a:r></a:p></p:txBody></p:sp>
<p:pic><p:spPr><a:xfrm><a:off x="952500" y="952500"/><a:ext cx="952500" cy="952500"/></a:xfrm></p:spPr><p:blipFill><a:blip r:embed="rId2"/></p:blipFill></p:pic>
</p:spTree></p:cSld></p:sld>`
	slideRels := `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/>
<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="../media/image1.png"/></Relationships>`
	layout := `<?xml version="1.0"?><p:sldLayout xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">
<p:cSld><p:spTree><p:sp><p:nvSpPr><p:cNvPr id="1" name="Title"/><p:cNvSpPr/><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr>
<p:spPr><a:xfrm><a:off x="609600" y="304800"/><a:ext cx="10972800" cy="1371600"/></a:xfrm></p:spPr><p:txBody/></p:sp></p:spTree></p:cSld></p:sldLayout>`
	layoutRels := `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster" Target="../slideMasters/slideMaster1.xml"/></Relationships>`
	master := `<?xml version="1.0"?><p:sldMaster xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld><p:spTree/></p:cSld></p:sldMaster>`
	theme := `<?xml version="1.0"?><a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:themeElements>
<a:clrScheme name="Office"><a:dk1><a:sysClr val="windowText" lastClr="000000"/></a:dk1><a:lt1><a:sysClr val="window" lastClr="FFFFFF"/></a:lt1><a:dk2><a:srgbClr val="44546A"/></a:dk2><a:lt2><a:srgbClr val="E7E6E6"/></a:lt2><a:accent1><a:srgbClr val="4472C4"/></a:accent1><a:accent2><a:srgbClr val="ED7D31"/></a:accent2><a:accent3><a:srgbClr val="A5A5A5"/></a:accent3><a:accent4><a:srgbClr val="FFC000"/></a:accent4><a:accent5><a:srgbClr val="5B9BD5"/></a:accent5><a:accent6><a:srgbClr val="70AD47"/></a:accent6></a:clrScheme>
<a:fontScheme name="Office"><a:majorFont><a:latin typeface="Calibri Light"/></a:majorFont><a:minorFont><a:latin typeface="Calibri"/></a:minorFont></a:fontScheme></a:themeElements></a:theme>`
	files := map[string]string{
		"ppt/presentation.xml":                         presentation,
		"ppt/_rels/presentation.xml.rels":              presRels,
		"ppt/slides/slide1.xml":                        slide,
		"ppt/slides/_rels/slide1.xml.rels":             slideRels,
		"ppt/slideLayouts/slideLayout1.xml":            layout,
		"ppt/slideLayouts/_rels/slideLayout1.xml.rels": layoutRels,
		"ppt/slideMasters/slideMaster1.xml":            master,
		"ppt/theme/theme1.xml":                         theme,
	}
	p := writeZip(t, "deck.pptx", files)
	addZipFile(t, p, "ppt/media/image1.png", tinyPNG)

	htmlOut, err := Render(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Deck title", "viewBox=\"0 0 1280 720\"", "data:image/png;base64,",
		"1 / 1", "font-size:53.33px", "font-weight:700", "Calibri Light",
		"x=\"64.00\"", // layout title x: 609600/9525
	} {
		if !strings.Contains(htmlOut, want) {
			t.Fatalf("rendered PPTX lacks %q\n%s", want, htmlOut)
		}
	}
}

// addZipFile rewrites path with name added.
func addZipFile(t *testing.T, path, name, b64 string) {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range zr.File {
		w, err := zw.Create(f.Name)
		if err != nil {
			t.Fatal(err)
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
		rc.Close()
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatal(err)
	}
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr.Close()
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}
