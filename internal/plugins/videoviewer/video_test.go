package videoviewer

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestServeLocalVideo(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "clip #1.mp4")
	content := []byte("not a real video, only served bytes")
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatal(err)
	}

	urlStr, stop, err := serveLocalVideo(p)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	resp, err := http.Get(urlStr)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "video/mp4" {
		t.Fatalf("content type = %q, want video/mp4", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != string(content) {
		t.Fatalf("served %q, want %q", body, content)
	}

	u, err := url.Parse(urlStr)
	if err != nil {
		t.Fatal(err)
	}
	bad, err := http.Get("http://" + u.Host + "/not-the-token")
	if err != nil {
		t.Fatal(err)
	}
	defer bad.Body.Close()
	if bad.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown path status = %d, want 404", bad.StatusCode)
	}
}

func TestVideoContentType(t *testing.T) {
	cases := map[string]string{
		"a.mp4":  "video/mp4",
		"a.webm": "video/webm",
		"a.mkv":  "video/x-matroska",
		"a.bin":  "application/octet-stream",
	}
	for name, want := range cases {
		if got := videoContentType(name); got != want {
			t.Fatalf("videoContentType(%q) = %q, want %q", name, got, want)
		}
	}
}
