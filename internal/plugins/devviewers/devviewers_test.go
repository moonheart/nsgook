package devviewers

import (
	"archive/zip"
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInspectCertEMLAndMesh(t *testing.T) {
	// Certificate generation + inspection
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(12345),
		Subject:      pkix.Name{CommonName: "nsgook.example.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		DNSNames:     []string{"nsgook.example.test"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	crep := InspectCertBytes(pemBytes, "Summary", "Valid", "Expired", "NotYet")
	if crep.BadgeAccent != "Valid" || len(crep.Sections) == 0 {
		t.Fatalf("unexpected cert report: %+v", crep)
	}

	// EML parsing
	eml := "Subject: Hello QuickLook\r\nFrom: a@example.com\r\nContent-Type: text/plain\r\n\r\nEmail body text."
	mrep, err := InspectEMLBytes([]byte(eml), "Summary", "Attachments")
	if err != nil || !strings.Contains(mrep.BodyText, "Email body text") {
		t.Fatalf("unexpected eml report: %+v err=%v", mrep, err)
	}

	// 3D OBJ + binary STL mesh parsing
	obj := "v 0 0 0\nv 1 0 0\nv 0 1 0\nf 1 2 3\n"
	mesh, err := ParseMeshBytes(".obj", []byte(obj))
	if err != nil || len(mesh.Vertices) != 3 || len(mesh.Faces) != 1 {
		t.Fatalf("unexpected OBJ mesh: %+v err=%v", mesh, err)
	}

	var stl bytes.Buffer
	stl.Write(make([]byte, 80))
	_ = binary.Write(&stl, binary.LittleEndian, uint32(1))
	stl.Write(make([]byte, 12))
	_ = binary.Write(&stl, binary.LittleEndian, [3]float32{0, 0, 0})
	_ = binary.Write(&stl, binary.LittleEndian, [3]float32{1, 0, 0})
	_ = binary.Write(&stl, binary.LittleEndian, [3]float32{0, 1, 0})
	_ = binary.Write(&stl, binary.LittleEndian, uint16(0))
	smesh, err := ParseMeshBytes(".stl", stl.Bytes())
	if err != nil || len(smesh.Faces) != 1 {
		t.Fatalf("unexpected STL mesh: %+v err=%v", smesh, err)
	}
}

func TestInspectDocxAndSQLiteAndWinFormats(t *testing.T) {
	dir := t.TempDir()

	// DOCX zip
	docxPath := filepath.Join(dir, "doc.docx")
	var dbuf bytes.Buffer
	zw := zip.NewWriter(&dbuf)
	w, _ := zw.Create("word/document.xml")
	_, _ = w.Write([]byte(`<?xml version="1.0"?><w:document><w:body><w:p><w:r><w:t>Hello NSGook Word!</w:t></w:r></w:p></w:body></w:document>`))
	_ = zw.Close()
	_ = os.WriteFile(docxPath, dbuf.Bytes(), 0o644)

	drep, err := InspectOffice(docxPath, "Summary", "Sheets", "Slides")
	if err != nil || !strings.Contains(drep.BodyText, "Hello NSGook Word!") {
		t.Fatalf("unexpected docx report: %+v err=%v", drep, err)
	}

	// SQLite page-1 header
	dbPath := filepath.Join(dir, "test.sqlite")
	page := make([]byte, 4096)
	copy(page[:16], "SQLite format 3\x00")
	binary.BigEndian.PutUint16(page[16:18], 4096)
	binary.BigEndian.PutUint32(page[28:32], 1)
	page[100] = 0x0D
	_ = os.WriteFile(dbPath, page, 0o644)

	srep, err := InspectSQLite(dbPath, "Summary", "Tables")
	if err != nil || srep.TitleSuffix != "SQLite 3" {
		t.Fatalf("unexpected sqlite report: %+v err=%v", srep, err)
	}

	// MDMP header
	mdmp := make([]byte, 44)
	copy(mdmp[:4], "MDMP")
	binary.LittleEndian.PutUint32(mdmp[8:12], 1)
	binary.LittleEndian.PutUint32(mdmp[12:16], 32)
	binary.LittleEndian.PutUint32(mdmp[32:36], 6) // ExceptionStream
	binary.LittleEndian.PutUint32(mdmp[36:40], 128)
	binary.LittleEndian.PutUint32(mdmp[40:44], 64)
	dumprep, err := InspectDumpBytes(mdmp, "Summary", "Streams")
	if err != nil || len(dumprep.Sections[1].Rows) != 1 || dumprep.Sections[1].Rows[0].Key != "ExceptionStream" {
		t.Fatalf("unexpected dump report: %+v err=%v", dumprep, err)
	}

	// LNK & ITSF headers
	lnk := make([]byte, 80)
	binary.LittleEndian.PutUint32(lnk[0:4], 0x4C)
	lrep := InspectLNKBytes(lnk, "Summary")
	if lrep.TitleSuffix != "Shortcut" {
		t.Fatalf("unexpected lnk report: %+v", lrep)
	}

	itsf := make([]byte, 32)
	copy(itsf[:4], "ITSF")
	binary.LittleEndian.PutUint32(itsf[4:8], 3)
	crep, err := InspectCHMBytes(itsf, "Summary")
	if err != nil || crep.TitleSuffix != "CHM" {
		t.Fatalf("unexpected chm report: %+v err=%v", crep, err)
	}
}
