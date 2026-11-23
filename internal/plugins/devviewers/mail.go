package devviewers

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf16"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"

	"nsgook/internal/plugins/pluginui"
	"nsgook/internal/viewer"
	"nsgook/locales"
)

var mailExts = map[string]bool{".eml": true, ".msg": true}

// MailPlugin previews .eml (RFC 5322 + MIME) and .msg (OLE2 Outlook) messages.
type MailPlugin struct{}

func (MailPlugin) ID() string              { return "mailviewer" }
func (MailPlugin) Name() string            { return "MailViewer" }
func (MailPlugin) Priority() int           { return 82 }
func (MailPlugin) CanHandle(p string) bool { return hasExt(p, mailExts) }

func (MailPlugin) Prepare(path string, ctx *viewer.Context) {
	ctx.PreferredSize = core.Size{Width: 820, Height: 620}
	ctx.Title.Set(viewer.DisplayName(path))
}

func (MailPlugin) View(cx *core.Scope, path string, ctx *viewer.Context) core.Node {
	tr := locales.For(cx)
	return renderReportViewer(cx, path, ctx, lucide.Mail, func() (*Report, error) {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if strings.EqualFold(filepath.Ext(path), ".msg") {
			return InspectMSGBytes(raw, tr.DevSummary()), nil
		}
		return InspectEMLBytes(raw, tr.DevSummary(), tr.DevAttachments())
	})
}

// InspectEMLBytes parses RFC 5322 email headers, plain/HTML parts, and attachments.
func InspectEMLBytes(raw []byte, titleSummary, titleAttachments string) (*Report, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	dec := &mime.WordDecoder{}
	decodeHdr := func(k string) string {
		v := msg.Header.Get(k)
		if v == "" {
			return ""
		}
		if s, derr := dec.DecodeHeader(v); derr == nil {
			return s
		}
		return v
	}

	var rows []pluginui.KV
	for _, k := range []string{"Subject", "From", "To", "Cc", "Date"} {
		if v := decodeHdr(k); v != "" {
			rows = append(rows, pluginui.KV{Key: k, Value: v})
		}
	}

	var plainBody, htmlBody string
	var attachments []pluginui.KV

	var walkPart func(ct string, cte string, body []byte)
	walkPart = func(ct, cte string, body []byte) {
		mediaType, params, _ := mime.ParseMediaType(ct)
		if mediaType == "" {
			mediaType = "text/plain"
		}
		if strings.HasPrefix(mediaType, "multipart/") && params["boundary"] != "" {
			mr := multipart.NewReader(bytes.NewReader(body), params["boundary"])
			for {
				p, err := mr.NextPart()
				if err != nil {
					break
				}
				pb, _ := io.ReadAll(io.LimitReader(p, 8<<20))
				fn := p.FileName()
				if fn != "" {
					attachments = append(attachments, pluginui.KV{Key: fn, Value: viewer.FormatBytes(uint64(len(pb)))})
				} else {
					walkPart(p.Header.Get("Content-Type"), p.Header.Get("Content-Transfer-Encoding"), pb)
				}
				_ = p.Close()
			}
			return
		}
		decoded := decodeBodyCTE(body, cte)
		switch {
		case mediaType == "text/plain" && plainBody == "":
			plainBody = string(decoded)
		case mediaType == "text/html" && htmlBody == "":
			htmlBody = string(decoded)
		}
	}

	fullBody, _ := io.ReadAll(io.LimitReader(msg.Body, 8<<20))
	walkPart(msg.Header.Get("Content-Type"), msg.Header.Get("Content-Transfer-Encoding"), fullBody)

	bodyText := plainBody
	if bodyText == "" && htmlBody != "" {
		bodyText = stripMailHTML(htmlBody)
	}

	secs := []Section{{Title: titleSummary, Rows: rows}}
	if len(attachments) > 0 {
		secs = append(secs, Section{Title: titleAttachments, Rows: attachments})
	}
	return &Report{
		TitleSuffix: decodeHdr("Subject"),
		Badges:      []string{"EML"},
		Sections:    secs,
		BodyText:    strings.TrimSpace(bodyText),
	}, nil
}

func decodeBodyCTE(b []byte, cte string) []byte {
	switch strings.ToLower(strings.TrimSpace(cte)) {
	case "base64":
		cleaned := bytes.Map(func(r rune) rune {
			if r == '\r' || r == '\n' || r == ' ' {
				return -1
			}
			return r
		}, b)
		if out, err := base64.StdEncoding.DecodeString(string(cleaned)); err == nil {
			return out
		}
	case "quoted-printable":
		if out, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(b))); err == nil {
			return out
		}
	}
	return b
}

var htmlTagRe = regexp.MustCompile(`<[^>]+>`)

func stripMailHTML(s string) string {
	s = strings.ReplaceAll(s, "<br>", "\n")
	s = strings.ReplaceAll(s, "<br/>", "\n")
	s = strings.ReplaceAll(s, "</p>", "\n\n")
	s = htmlTagRe.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

// InspectMSGBytes scans an Outlook OLE2 .msg compound binary for readable UTF-16LE message strings.
func InspectMSGBytes(raw []byte, titleSummary string) *Report {
	var strs []string
	// Extract contiguous printable UTF-16LE sequences >= 6 characters from OLE streams.
	var u []uint16
	flush := func() {
		if len(u) >= 6 {
			s := strings.TrimSpace(string(utf16.Decode(u)))
			if len(s) >= 6 {
				strs = append(strs, s)
			}
		}
		u = u[:0]
	}
	for i := 0; i+1 < len(raw); i += 2 {
		v := uint16(raw[i]) | (uint16(raw[i+1]) << 8)
		if (v >= 0x20 && v <= 0x7E) || v == '\n' || v == '\r' || v == '\t' || (v >= 0x4E00 && v <= 0x9FFF) {
			u = append(u, v)
		} else {
			flush()
		}
	}
	flush()

	var body strings.Builder
	seen := make(map[string]bool)
	for _, s := range strs {
		if strings.HasPrefix(s, "__substg") || strings.HasPrefix(s, "Root Entry") || seen[s] {
			continue
		}
		seen[s] = true
		body.WriteString(s)
		body.WriteString("\n\n")
		if body.Len() > 32*1024 {
			break
		}
	}
	return &Report{
		TitleSuffix: "Outlook MSG",
		Badges:      []string{"MSG", "OLE2"},
		Sections: []Section{
			{Title: titleSummary, Rows: []pluginui.KV{
				{Key: "Format", Value: "Microsoft Outlook Binary Message (.msg)"},
				{Key: "Size", Value: viewer.FormatBytes(uint64(len(raw)))},
			}},
		},
		BodyText: strings.TrimSpace(body.String()),
	}
}
