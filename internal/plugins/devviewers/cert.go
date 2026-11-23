package devviewers

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"

	"nsgook/internal/plugins/pluginui"
	"nsgook/internal/viewer"
	"nsgook/locales"
)

var certExts = map[string]bool{
	".pem": true, ".crt": true, ".cer": true, ".der": true,
	".p7b": true, ".p7c": true, ".csr": true, ".pub": true,
}

// CertPlugin inspects X.509 certificates, CSRs, and PEM blocks using crypto/x509.
type CertPlugin struct{}

func (CertPlugin) ID() string              { return "certviewer" }
func (CertPlugin) Name() string            { return "CertViewer" }
func (CertPlugin) Priority() int           { return 82 }
func (CertPlugin) CanHandle(p string) bool { return hasExt(p, certExts) }

func (CertPlugin) Prepare(path string, ctx *viewer.Context) {
	ctx.PreferredSize = core.Size{Width: 740, Height: 560}
	ctx.Title.Set(viewer.DisplayName(path))
}

func (CertPlugin) View(cx *core.Scope, path string, ctx *viewer.Context) core.Node {
	tr := locales.For(cx)
	return renderReportViewer(cx, path, ctx, lucide.ShieldCheck, func() (*Report, error) {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return InspectCertBytes(raw, tr.DevSummary(), tr.DevValid(), tr.DevExpired(), tr.DevNotYetValid()), nil
	})
}

// InspectCertBytes parses PEM or DER X.509 certificates and CSRs into a structured report.
func InspectCertBytes(raw []byte, titleSummary, lblValid, lblExpired, lblNotYet string) *Report {
	var blocks []*pem.Block
	rest := raw
	for {
		var blk *pem.Block
		blk, rest = pem.Decode(rest)
		if blk == nil {
			break
		}
		blocks = append(blocks, blk)
	}
	if len(blocks) == 0 {
		// Treat raw bytes as DER certificate or CSR.
		blocks = append(blocks, &pem.Block{Type: "DER", Bytes: raw})
	}

	now := time.Now()
	var sections []Section
	var badges []string
	statusBadge := ""

	for idx, blk := range blocks {
		if cert, err := x509.ParseCertificate(blk.Bytes); err == nil {
			sum := sha256.Sum256(cert.Raw)
			if statusBadge == "" {
				switch {
				case now.After(cert.NotAfter):
					statusBadge = lblExpired
				case now.Before(cert.NotBefore):
					statusBadge = lblNotYet
				default:
					statusBadge = lblValid
				}
			}
			badges = append(badges, cert.SignatureAlgorithm.String(), cert.PublicKeyAlgorithm.String())
			rows := []pluginui.KV{
				{Key: "Subject", Value: cert.Subject.String()},
				{Key: "Issuer", Value: cert.Issuer.String()},
				{Key: "Serial", Value: cert.SerialNumber.Text(16)},
				{Key: "Valid From", Value: cert.NotBefore.Format("2006-01-02 15:04:05 MST")},
				{Key: "Valid Until", Value: cert.NotAfter.Format("2006-01-02 15:04:05 MST")},
				{Key: "Key / Sig", Value: fmt.Sprintf("%s · %s", cert.PublicKeyAlgorithm, cert.SignatureAlgorithm)},
				{Key: "CA", Value: fmt.Sprintf("%v", cert.IsCA)},
				{Key: "SHA-256", Value: strings.ToUpper(hex.EncodeToString(sum[:]))},
			}
			if len(cert.DNSNames) > 0 {
				rows = append(rows, pluginui.KV{Key: "DNS SANs", Value: strings.Join(cert.DNSNames, ", ")})
			}
			if len(cert.IPAddresses) > 0 {
				ips := make([]string, len(cert.IPAddresses))
				for i, ip := range cert.IPAddresses {
					ips[i] = ip.String()
				}
				rows = append(rows, pluginui.KV{Key: "IP SANs", Value: strings.Join(ips, ", ")})
			}
			title := titleSummary
			if len(blocks) > 1 {
				title = fmt.Sprintf("Certificate #%d", idx+1)
			}
			sections = append(sections, Section{Title: title, Rows: rows})
			continue
		}
		if csr, err := x509.ParseCertificateRequest(blk.Bytes); err == nil {
			rows := []pluginui.KV{
				{Key: "Kind", Value: "X.509 Certificate Signing Request (CSR)"},
				{Key: "Subject", Value: csr.Subject.String()},
				{Key: "Algorithm", Value: fmt.Sprintf("%s · %s", csr.PublicKeyAlgorithm, csr.SignatureAlgorithm)},
			}
			if len(csr.DNSNames) > 0 {
				rows = append(rows, pluginui.KV{Key: "DNS SANs", Value: strings.Join(csr.DNSNames, ", ")})
			}
			sections = append(sections, Section{Title: "CSR", Rows: rows})
			continue
		}
		sum := sha256.Sum256(blk.Bytes)
		sections = append(sections, Section{
			Title: blk.Type,
			Rows: []pluginui.KV{
				{Key: "PEM Block", Value: blk.Type},
				{Key: "Bytes", Value: fmt.Sprintf("%d", len(blk.Bytes))},
				{Key: "SHA-256", Value: hex.EncodeToString(sum[:])},
			},
		})
	}

	return &Report{
		TitleSuffix: "X.509",
		BadgeAccent: statusBadge,
		Badges:      badges,
		Sections:    sections,
	}
}
