package devviewers

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"nsgook/internal/plugins/pluginui"
	"nsgook/internal/viewer"
	"nsgook/locales"
)

var appPkgExts = map[string]bool{
	".apk": true, ".aab": true, ".ipa": true, ".xap": true,
	".appx": true, ".msix": true, ".deb": true, ".rpm": true,
}

// AppPlugin previews mobile/desktop installer packages (.apk, .aab, .ipa, .appx, .msix, .deb, .rpm).
type AppPlugin struct{}

func (AppPlugin) ID() string              { return "appviewer" }
func (AppPlugin) Name() string            { return "AppViewer" }
func (AppPlugin) Priority() int           { return 82 }
func (AppPlugin) CanHandle(p string) bool { return hasExt(p, appPkgExts) }

func (AppPlugin) Prepare(path string, ctx *viewer.Context) {
	ctx.PreferredSize = core.Size{Width: 780, Height: 600}
	ctx.Title.Set(viewer.DisplayName(path))
}

func (AppPlugin) View(cx *core.Scope, path string, ctx *viewer.Context) core.Node {
	tr := locales.For(cx)
	return renderReportViewer(cx, path, ctx, lucide.Package, func() (*Report, error) {
		return InspectAppPackage(path, tr.DevSummary(), tr.DevPermissions())
	})
}

// InspectAppPackage parses ZIP-based manifests (AppxManifest.xml, Info.plist, AndroidManifest.xml), .deb, and .rpm headers.
func InspectAppPackage(path, titleSummary, titlePerms string) (*Report, error) {
	ext := strings.ToLower(filepath.Ext(path))
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	if ext == ".deb" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		summary := []pluginui.KV{
			{Key: "Format", Value: "Debian Package (.deb)"},
			{Key: "Size", Value: viewer.FormatBytes(uint64(len(raw)))},
		}
		if bytes.HasPrefix(raw, []byte("!<arch>\n")) {
			summary = append(summary, pluginui.KV{Key: "Archive", Value: "UNIX ar archive"})
		}
		return &Report{TitleSuffix: "DEB", Badges: []string{"DEB"}, Sections: []Section{{Title: titleSummary, Rows: summary}}}, nil
	}

	if ext == ".rpm" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		summary := []pluginui.KV{
			{Key: "Format", Value: "RPM Package (.rpm)"},
			{Key: "Size", Value: viewer.FormatBytes(uint64(len(raw)))},
		}
		if len(raw) >= 96 && bytes.HasPrefix(raw, []byte{0xED, 0xAB, 0xEE, 0xDB}) {
			archName := strings.TrimRight(string(raw[10:76]), "\x00")
			summary = append(summary, pluginui.KV{Key: "Lead Name", Value: archName})
		}
		return &Report{TitleSuffix: "RPM", Badges: []string{"RPM"}, Sections: []Section{{Title: titleSummary, Rows: summary}}}, nil
	}

	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	var rows []pluginui.KV
	rows = append(rows,
		pluginui.KV{Key: "Package Type", Value: strings.TrimPrefix(strings.ToUpper(ext), ".")},
		pluginui.KV{Key: "Size", Value: viewer.FormatBytes(uint64(max(0, st.Size())))},
		pluginui.KV{Key: "Files", Value: fmt.Sprintf("%d", len(zr.File))},
	)

	var permRows []pluginui.KV
	var rawManifest string

	for _, f := range zr.File {
		low := strings.ToLower(f.Name)
		switch {
		case strings.EqualFold(f.Name, "AppxManifest.xml") || strings.EqualFold(f.Name, "WMAppManifest.xml"):
			if b, err := readZipLimit(f, 512*1024); err == nil {
				rawManifest = string(b)
				kvs, perms := parseXMLManifestAttrs(b)
				rows = append(rows, kvs...)
				permRows = append(permRows, perms...)
			}
		case strings.HasSuffix(low, ".app/info.plist") && len(rawManifest) == 0:
			if b, err := readZipLimit(f, 512*1024); err == nil && bytes.Contains(b, []byte("<plist")) {
				rawManifest = string(b)
			}
		case strings.EqualFold(f.Name, "AndroidManifest.xml"):
			if b, err := readZipLimit(f, 512*1024); err == nil {
				strs := extractAXMLStrings(b)
				for _, s := range strs {
					if strings.HasPrefix(s, "android.permission.") {
						permRows = append(permRows, pluginui.KV{Key: "Permission", Value: strings.TrimPrefix(s, "android.permission.")})
					} else if strings.Contains(s, ".") && !strings.ContainsAny(s, " /:\\") && len(rows) < 10 {
						rows = append(rows, pluginui.KV{Key: "Identifier", Value: s})
					}
				}
			}
		}
	}

	secs := []Section{{Title: titleSummary, Rows: rows}}
	if len(permRows) > 0 {
		secs = append(secs, Section{Title: titlePerms, Rows: permRows})
	}
	return &Report{
		TitleSuffix: strings.TrimPrefix(strings.ToUpper(ext), "."),
		Badges:      []string{strings.TrimPrefix(strings.ToUpper(ext), ".")},
		ImageData:   pickAppIcon(&zr.Reader),
		Sections:    secs,
		BodyText:    rawManifest,
	}, nil
}

// pickAppIcon chooses the icon a package shows: a composed icon when it has
// one, else the highest-density adaptive foreground and background layers
// composited together. Android APKs often ship only
// ic_launcher_foreground.png and ic_launcher_background.png, so taking the
// first matching file shows a blank background instead of the app's icon.
func pickAppIcon(zr *zip.Reader) []byte {
	type candidate struct {
		data  []byte
		score int
		size  int
	}
	var composed, foreground, background []candidate
	better := func(list []candidate, data []byte, score int) []candidate {
		return append(list, candidate{data: data, score: score, size: len(data)})
	}
	for _, f := range zr.File {
		low := strings.ToLower(f.Name)
		if !(strings.HasSuffix(low, ".png") || strings.HasSuffix(low, ".jpg") ||
			strings.HasSuffix(low, ".jpeg") || strings.HasSuffix(low, ".webp")) {
			continue
		}
		if !strings.Contains(low, "ic_launcher") && !strings.Contains(low, "appicon") && !strings.Contains(low, "storelogo") {
			continue
		}
		data, err := readZipLimit(f, 2<<20)
		if err != nil || len(data) == 0 {
			continue
		}
		score := iconDensityScore(low)
		switch {
		case strings.Contains(low, "_foreground"):
			foreground = better(foreground, data, score)
		case strings.Contains(low, "_background"):
			background = better(background, data, score)
		default:
			composed = better(composed, data, score)
		}
	}
	best := func(list []candidate) *candidate {
		var pick *candidate
		for i := range list {
			if pick == nil || list[i].score > pick.score || (list[i].score == pick.score && list[i].size > pick.size) {
				pick = &list[i]
			}
		}
		return pick
	}
	if c := best(composed); c != nil {
		if out := normalizeIcon(c.data); out != nil {
			return out
		}
	}
	fg, bg := best(foreground), best(background)
	if fg != nil && bg != nil {
		if out := composeIcon(bg.data, fg.data); out != nil {
			return out
		}
	}
	if fg != nil {
		return normalizeIcon(fg.data)
	}
	if bg != nil {
		return normalizeIcon(bg.data)
	}
	return nil
}

func iconDensityScore(low string) int {
	switch {
	case strings.Contains(low, "xxxhdpi"):
		return 5
	case strings.Contains(low, "xxhdpi"):
		return 4
	case strings.Contains(low, "xhdpi"):
		return 3
	case strings.Contains(low, "hdpi"):
		return 2
	case strings.Contains(low, "mdpi"):
		return 1
	default:
		return 0
	}
}

// normalizeIcon decodes an icon and re-encodes it as PNG so a file the image
// node cannot read leaves its card out instead of showing a blank rectangle.
func normalizeIcon(data []byte) []byte {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil
	}
	return out.Bytes()
}

// composeIcon draws an adaptive icon's foreground over its background. The
// layers share a size in an APK; a different foreground is scaled to fit.
func composeIcon(background, foreground []byte) []byte {
	bg, _, err := image.Decode(bytes.NewReader(background))
	if err != nil {
		return nil
	}
	fg, _, err := image.Decode(bytes.NewReader(foreground))
	if err != nil {
		return nil
	}
	bounds := bg.Bounds()
	canvas := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(canvas, canvas.Bounds(), bg, bounds.Min, draw.Src)
	if fg.Bounds().Size() == canvas.Bounds().Size() {
		draw.Draw(canvas, canvas.Bounds(), fg, fg.Bounds().Min, draw.Over)
	} else {
		xdraw.CatmullRom.Scale(canvas, canvas.Bounds(), fg, fg.Bounds(), draw.Over, nil)
	}
	var out bytes.Buffer
	if err := png.Encode(&out, canvas); err != nil {
		return nil
	}
	return out.Bytes()
}

func readZipLimit(f *zip.File, limit int64) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, limit))
}

func parseXMLManifestAttrs(data []byte) ([]pluginui.KV, []pluginui.KV) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var kvs, perms []pluginui.KV
	var curElem string
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			curElem = t.Name.Local
			for _, a := range t.Attr {
				switch a.Name.Local {
				case "Name", "Version", "Publisher", "DisplayName":
					if curElem == "Identity" || curElem == "Properties" || curElem == "Application" {
						kvs = append(kvs, pluginui.KV{Key: curElem + "." + a.Name.Local, Value: a.Value})
					}
				}
				if strings.Contains(strings.ToLower(curElem), "capability") && a.Name.Local == "Name" {
					perms = append(perms, pluginui.KV{Key: curElem, Value: a.Value})
				}
			}
		}
	}
	return kvs, perms
}

// extractAXMLStrings reads the UTF-8 or UTF-16LE string pool chunk inside Android binary AXML manifests.
func extractAXMLStrings(data []byte) []string {
	if len(data) < 36 || binary.LittleEndian.Uint16(data[0:2]) != 0x0003 {
		return nil
	}
	// Chunk 1 starts at byte 8; verify StringPool chunk type 0x0001.
	if binary.LittleEndian.Uint16(data[8:10]) != 0x0001 {
		return nil
	}
	strCount := int(binary.LittleEndian.Uint32(data[16:20]))
	flags := binary.LittleEndian.Uint32(data[24:28])
	stringsStart := 8 + int(binary.LittleEndian.Uint32(data[28:32]))
	isUTF8 := flags&(1<<8) != 0
	offsetsBase := 8 + 28
	var out []string
	for i := 0; i < strCount && offsetsBase+(i+1)*4 <= len(data); i++ {
		off := stringsStart + int(binary.LittleEndian.Uint32(data[offsetsBase+i*4:offsetsBase+(i+1)*4]))
		if off < stringsStart || off+2 > len(data) {
			continue
		}
		var s string
		if isUTF8 {
			// Skip UTF-16 char len, then read UTF-8 byte len.
			p := off
			if p < len(data) {
				if data[p]&0x80 != 0 {
					p += 2
				} else {
					p++
				}
			}
			if p >= len(data) {
				continue
			}
			uLen := int(data[p])
			if uLen&0x80 != 0 && p+1 < len(data) {
				uLen = ((uLen & 0x7F) << 8) | int(data[p+1])
				p += 2
			} else {
				p++
			}
			if p+uLen <= len(data) {
				s = string(data[p : p+uLen])
			}
		} else {
			uLen := int(binary.LittleEndian.Uint16(data[off : off+2]))
			p := off + 2
			if p+uLen*2 <= len(data) {
				u := make([]uint16, uLen)
				for k := 0; k < uLen; k++ {
					u[k] = binary.LittleEndian.Uint16(data[p+k*2 : p+k*2+2])
				}
				s = string(utf16.Decode(u))
			}
		}
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
