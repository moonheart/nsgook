package devviewers

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"

	"nsgook/internal/plugins/pluginui"
	"nsgook/internal/viewer"
	"nsgook/locales"
)

// MediaInfoPlugin provides deep container, codec, MIME, header-signature, and hash inspection.
// Low priority (-5) so format viewers take precedence, and always available via More -> "View File / Media Info".
type MediaInfoPlugin struct{}

func (MediaInfoPlugin) ID() string              { return "mediainfo" }
func (MediaInfoPlugin) Name() string            { return "MediaInfoViewer" }
func (MediaInfoPlugin) Priority() int           { return -5 }
func (MediaInfoPlugin) CanHandle(p string) bool { return p != "" && !strings.HasPrefix(p, "::") }

func (MediaInfoPlugin) Prepare(path string, ctx *viewer.Context) {
	ctx.PreferredSize = core.Size{Width: 740, Height: 540}
	ctx.Title.Set(viewer.DisplayName(path))
}

func (MediaInfoPlugin) ExtendedMenuItems(cx *core.Scope, _ string, ctx *viewer.Context) []viewer.MenuAction {
	if ctx.IsDir {
		return nil
	}
	return []viewer.MenuAction{
		{
			ID:    "ext-view-mediainfo",
			Label: locales.For(cx).ViewAsInfo(),
			Icon:  lucide.Info,
			OnSelect: func() {
				if ctx.SwitchPlugin != nil {
					ctx.SwitchPlugin("mediainfo")
				}
			},
		},
	}
}

func (MediaInfoPlugin) View(cx *core.Scope, path string, ctx *viewer.Context) core.Node {
	tr := locales.For(cx)
	return renderReportViewer(cx, path, ctx, lucide.Info, func() (*Report, error) {
		return InspectMediaAndFile(path, tr.DevSummary(), tr.DevRawDetails())
	})
}

// InspectMediaAndFile inspects container headers, magic signature, MIME type, timestamps, and SHA-256.
func InspectMediaAndFile(path, titleSummary, titleDetails string) (*Report, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	head := make([]byte, 8192)
	n, _ := io.ReadFull(f, head)
	head = head[:n]

	_, _ = f.Seek(0, io.SeekStart)
	h := sha256.New()
	_, _ = io.Copy(h, f)
	sum := strings.ToUpper(hex.EncodeToString(h.Sum(nil)))

	mimeType := http.DetectContentType(head)
	container, details := sniffContainer(head)

	summary := []pluginui.KV{
		{Key: "File Name", Value: filepath.Base(path)},
		{Key: "Extension", Value: filepath.Ext(path)},
		{Key: "Container / Sig", Value: container},
		{Key: "MIME Sniff", Value: mimeType},
		{Key: "Size", Value: fmt.Sprintf("%s (%d B)", viewer.FormatBytes(uint64(max(0, st.Size()))), st.Size())},
		{Key: "Modified", Value: st.ModTime().Format("2006-01-02 15:04:05")},
		{Key: "Permissions", Value: st.Mode().String()},
		{Key: "SHA-256", Value: sum},
	}

	secs := []Section{{Title: titleSummary, Rows: summary}}
	if len(details) > 0 {
		secs = append(secs, Section{Title: titleDetails, Rows: details})
	}
	return &Report{
		TitleSuffix: container,
		Badges:      []string{container, mimeType},
		Sections:    secs,
	}, nil
}

func sniffContainer(b []byte) (string, []pluginui.KV) {
	if len(b) < 4 {
		return "Empty / Small File", nil
	}
	switch {
	case len(b) >= 24 && string(b[:8]) == "\x89PNG\r\n\x1a\n":
		w := binary.BigEndian.Uint32(b[16:20])
		h := binary.BigEndian.Uint32(b[20:24])
		return "PNG Image", []pluginui.KV{{Key: "Dimensions", Value: fmt.Sprintf("%d × %d px", w, h)}}
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return "JPEG Image", nil
	case len(b) >= 12 && string(b[:4]) == "RIFF":
		sub := string(b[8:12])
		if sub == "WAVE" && len(b) >= 36 {
			ch := binary.LittleEndian.Uint16(b[22:24])
			sr := binary.LittleEndian.Uint32(b[24:28])
			return "RIFF / WAVE Audio", []pluginui.KV{{Key: "Audio", Value: fmt.Sprintf("%d Hz · %d ch", sr, ch)}}
		}
		return "RIFF / " + sub, nil
	case len(b) >= 12 && string(b[4:8]) == "ftyp":
		brand := strings.TrimSpace(string(b[8:12]))
		return "ISO Base Media (MP4/MOV)", []pluginui.KV{{Key: "Major Brand", Value: brand}}
	case string(b[:4]) == "\x1a\x45\xdf\xa3":
		return "EBML / Matroska / WebM", nil
	case string(b[:4]) == "fLaC":
		return "FLAC Lossless Audio", nil
	case string(b[:3]) == "ID3":
		return "MP3 / ID3v2 Audio", nil
	case string(b[:4]) == "OggS":
		return "Ogg / Vorbis / Opus", nil
	case string(b[:4]) == "%PDF":
		return "PDF Document", nil
	case string(b[:4]) == "PK\x03\x04":
		return "ZIP / OOXML Container", nil
	case string(b[:2]) == "MZ":
		return "Windows PE Executable / DLL", nil
	case string(b[:4]) == "\x7fELF":
		return "UNIX ELF Binary", nil
	case len(b) >= 16 && string(b[:16]) == "SQLite format 3\x00":
		return "SQLite 3 Database", nil
	}
	return "Generic File", nil
}
