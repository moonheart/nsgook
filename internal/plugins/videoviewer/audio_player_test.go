package videoviewer

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"nsgook/internal/viewer"
)

func TestLocalFileURI(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows file path syntax")
	}
	cases := map[string]string{
		`C:\Music\Track 01.mp3`:      "file:///C:/Music/Track%2001.mp3",
		`C:\音乐\测试 #1.mp3`:            "file:///C:/音乐/测试%20%231.mp3",
		`C:\Music\100%.mp3`:          "file:///C:/Music/100%25.mp3",
		`\\server\share\song.mp3`:    "file://server/share/song.mp3",
		`D:\Podcasts\episode-01.m4a`: "file:///D:/Podcasts/episode-01.m4a",
	}
	for path, want := range cases {
		if got := localFileURI(path); got != want {
			t.Fatalf("localFileURI(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestAudioViewUsesPlayer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tone.mp3")
	if err := os.WriteFile(path, []byte("headless audio source"), 0o644); err != nil {
		t.Fatal(err)
	}

	uri := localFileURI(path)
	var vctx *viewer.Context
	a := nsgotest.StartApp(t, func(cx *core.Scope) {
		vctx = viewer.NewContext(cx, nil, nil)
		cx.SetMainView(func(cx *core.Scope) core.Node {
			return Plugin{}.View(cx, path, vctx)
		})
	},
		nsgotest.Environment(core.Environment{OS: core.OSWindows, Audio: true}),
		nsgotest.Size(600, 480),
		func(a *nsgotest.App) {
			a.AudioDurations = map[string]time.Duration{uri: 8 * time.Second}
		},
	)
	a.WaitFor("id:media-play", 2*time.Second)

	if len(a.Players) != 1 {
		t.Fatalf("opened %d players, want 1", len(a.Players))
	}
	p := a.Players[0]
	if p.Source.URI != uri {
		t.Fatalf("player source = %q, want %q", p.Source.URI, uri)
	}
	if !p.Loaded || !p.Playing {
		t.Fatalf("player loaded=%v playing=%v, want both true", p.Loaded, p.Playing)
	}
	if p.Duration != 8*time.Second {
		t.Fatalf("player duration = %v, want 8s", p.Duration)
	}

	// Pausing and resuming go through the player, not a video node.
	a.Press("id:media-play")
	a.Settle()
	if p.Playing {
		t.Fatal("player still playing after pause")
	}
	a.Press("id:media-play")
	a.Settle()
	if !p.Playing {
		t.Fatal("player not playing after resume")
	}

	a.Press("id:media-speed")
	a.Settle()
	if p.Rate != 1.25 {
		t.Fatalf("player rate = %v, want 1.25", p.Rate)
	}

	a.Press("id:media-loop")
	a.Settle()
	if !p.Loop {
		t.Fatal("player loop not enabled")
	}

	a.Press("id:media-mute")
	a.Settle()
	if p.Volume != 0 {
		t.Fatalf("player volume = %v, want 0 after mute", p.Volume)
	}

	// The player closes with the preview, not after the process ends.
	a.Post(func() { vctx.Releasing.Set(true) })
	a.Settle()
	if !p.Closed {
		t.Fatal("player not closed when the preview releases media")
	}
}
