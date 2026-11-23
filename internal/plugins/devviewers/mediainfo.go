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
	"sync/atomic"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

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
	colors := ui.ColorsOf(cx)

	hashState := core.NewSignal(cx, "") // "", "computing", or hex digest
	var stopped atomic.Bool
	cx.Cleanup(func() {
		stopped.Store(true)
	})
	cx.Effect(func() {
		if ctx.IsReleasing() {
			stopped.Store(true)
		}
	})

	computeHash := func() {
		if hashState.GetUntracked() != "" {
			return
		}
		hashState.Set("computing")
		go func() {
			sum, err := hashFileSHA256(path, stopped.Load)
			if stopped.Load() {
				return
			}
			cx.Post(func() {
				if stopped.Load() {
					return
				}
				if err != nil {
					hashState.Set("")
					if ctx.Toast != nil {
						ctx.Toast(ui.ToastError, err.Error())
					}
					return
				}
				hashState.Set(sum)
			})
		}()
	}

	report := renderReportViewer(cx, path, ctx, lucide.Info, func() (*Report, error) {
		return InspectMediaAndFile(path, tr.DevSummary(), tr.DevRawDetails())
	})

	hashRow := core.View{
		Style: core.Style{
			Direction:  core.Row,
			AlignItems: core.AlignCenter,
			Gap:        8,
			Padding:    core.Symmetric(8, 14),
			Borders:    core.Edges{Top: 1},
			FlexShrink: 0,
		},
		StyleFunc: func() core.Style {
			return core.Style{
				Background:  ui.Alpha(colors.Surface(), 0.78),
				BorderColor: colors.Line(),
			}
		},
		Children: []core.Node{
			core.Text{
				Content:   "SHA-256",
				Style:     core.Style{Width: core.Pt(130), FlexShrink: 0, FontSize: 12, FontWeight: ui.Medium},
				StyleFunc: ui.Colored(colors.Muted),
			},
			core.Show{
				When: func() bool { return hashState.Get() == "" },
				Then: func(cx *core.Scope) core.Node {
					return ui.Button{
						Text:    locales.For(cx).InfoComputeHash(),
						Icon:    lucide.FingerprintPattern,
						Variant: ui.ButtonSecondary,
						Size:    ui.SizeSmall,
						OnPress: computeHash,
					}
				},
			},
			core.Show{
				When: func() bool { return hashState.Get() == "computing" },
				Then: func(cx *core.Scope) core.Node {
					return core.View{
						Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6},
						Children: []core.Node{
							ui.Spinner{Size: 14},
							ui.Text(locales.For(cx).InfoComputingHash(), 12, ui.Regular, colors.Muted),
						},
					}
				},
			},
			core.Show{
				When: func() bool {
					s := hashState.Get()
					return s != "" && s != "computing"
				},
				Then: func(cx *core.Scope) core.Node {
					return core.View{
						Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
						Children: []core.Node{
							core.Text{
								ContentFunc: func() string { return hashState.Get() },
								MaxLines:    1,
								Selectable:  core.SelectableOn,
								Style:       core.Style{FontSize: 11, FontFamily: ui.Mono, FlexShrink: 1},
								StyleFunc:   ui.Colored(colors.Text),
							},
							pluginui.IconButton{
								Icon:     lucide.Copy,
								Label:    locales.For(cx).InfoCopyHash(),
								Size:     24,
								IconSize: 13,
								OnPress: func() {
									cx.SetClipboard(hashState.GetUntracked())
									if ctx.Toast != nil {
										ctx.Toast(ui.ToastSuccess, locales.For(cx).InfoHashCopied())
									}
								},
							},
						},
					}
				},
			},
		},
	}

	return pluginui.Fill(core.View{
		Style: core.Style{
			Direction:  core.Column,
			FlexGrow:   1,
			FlexShrink: 1,
			MinWidth:   core.Pt(0),
			MinHeight:  core.Pt(0),
		},
		Children: []core.Node{report, hashRow},
	})
}

// InspectMediaAndFile inspects container headers, magic signature, MIME type, timestamps, and file metadata.
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

// hashFileSHA256 computes the uppercase SHA-256 of path, stopping early when
// cancelled reports true.
func hashFileSHA256(path string, cancelled func() bool) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	buf := make([]byte, 64*1024)
	for {
		if cancelled != nil && cancelled() {
			return "", nil
		}
		n, readErr := f.Read(buf)
		if n > 0 {
			_, _ = h.Write(buf[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}
	return strings.ToUpper(hex.EncodeToString(h.Sum(nil))), nil
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
