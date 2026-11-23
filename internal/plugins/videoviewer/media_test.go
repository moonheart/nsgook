package videoviewer

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseLRCAndActiveIndex(t *testing.T) {
	lrc := "[00:05.50][00:01.00]First chorus\n[00:10.000]Second line\n"
	lines := ParseLRC(lrc)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lyric lines, got %d", len(lines))
	}
	if lines[0].Time != time.Second || lines[1].Time != 5500*time.Millisecond {
		t.Fatalf("unexpected sorted times: %+v", lines)
	}
	if got := ActiveLyricIndex(lines, 6*time.Second); got != 1 {
		t.Fatalf("expected active index 1 at 6s, got %d", got)
	}
	if got := ActiveLyricIndex(lines, 500*time.Millisecond); got != -1 {
		t.Fatalf("expected active index -1 at 0.5s, got %d", got)
	}
}

func TestInspectWAVAndID3v2AndFLAC(t *testing.T) {
	dir := t.TempDir()

	// Synthetic WAV: 44100 Hz stereo 16-bit PCM, 44100*4 bytes = 1 second.
	wav := make([]byte, 44+176400)
	copy(wav[0:4], "RIFF")
	binary.LittleEndian.PutUint32(wav[4:8], uint32(len(wav)-8))
	copy(wav[8:12], "WAVE")
	copy(wav[12:16], "fmt ")
	binary.LittleEndian.PutUint32(wav[16:20], 16)
	binary.LittleEndian.PutUint16(wav[20:22], 1)
	binary.LittleEndian.PutUint16(wav[22:24], 2)
	binary.LittleEndian.PutUint32(wav[24:28], 44100)
	binary.LittleEndian.PutUint32(wav[28:32], 176400)
	binary.LittleEndian.PutUint16(wav[32:34], 4)
	binary.LittleEndian.PutUint16(wav[34:36], 16)
	copy(wav[36:40], "data")
	binary.LittleEndian.PutUint32(wav[40:44], 176400)
	wavPath := filepath.Join(dir, "tone.wav")
	if err := os.WriteFile(wavPath, wav, 0o644); err != nil {
		t.Fatal(err)
	}
	wm := InspectMedia(wavPath)
	if wm.SampleRate != 44100 || wm.Channels != 2 || wm.Duration != time.Second {
		t.Fatalf("unexpected wav metadata: %+v", wm)
	}

	// Synthetic ID3v2.3 MP3 with TIT2 and TPE1.
	frameTitle := append([]byte{3}, []byte("Track One")...)
	frameArtist := append([]byte{3}, []byte("Artist Two")...)
	var body []byte
	for _, f := range []struct {
		id string
		p  []byte
	}{{"TIT2", frameTitle}, {"TPE1", frameArtist}} {
		body = append(body, []byte(f.id)...)
		var sz [4]byte
		binary.BigEndian.PutUint32(sz[:], uint32(len(f.p)))
		body = append(body, sz[:]...)
		body = append(body, 0, 0)
		body = append(body, f.p...)
	}
	hdr := []byte{'I', 'D', '3', 3, 0, 0, 0, 0, 0, byte(len(body))}
	mp3Path := filepath.Join(dir, "song.mp3")
	if err := os.WriteFile(mp3Path, append(hdr, body...), 0o644); err != nil {
		t.Fatal(err)
	}
	mm := InspectMedia(mp3Path)
	if mm.Title != "Track One" || mm.Artist != "Artist Two" {
		t.Fatalf("unexpected mp3 metadata: %+v", mm)
	}

	// Synthetic FLAC with STREAMINFO.
	si := make([]byte, 34)
	// 48000 Hz = 0x0BB80, ch=2 (1), bps=24 (23), samples=96000 (2s).
	si[10] = 0x0B
	si[11] = 0xB8
	si[12] = 0x00 | (1 << 1) | 1 // ch=2, bps high bit=1
	si[13] = (7 << 4)            // low 4 bits of (bps-1)=23 -> 7
	binary.BigEndian.PutUint32(si[14:18], 96000)
	flac := []byte{'f', 'L', 'a', 'C', 0x80, 0, 0, 34}
	flac = append(flac, si...)
	flacPath := filepath.Join(dir, "clip.flac")
	if err := os.WriteFile(flacPath, flac, 0o644); err != nil {
		t.Fatal(err)
	}
	fm := InspectMedia(flacPath)
	if fm.SampleRate != 48000 || fm.Channels != 2 || fm.Duration != 2*time.Second {
		t.Fatalf("unexpected flac metadata: %+v", fm)
	}
}
