// Package videoviewer implements NSGook's video and audio preview plugin.
package videoviewer

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

var videoExts = map[string]bool{
	".mp4": true, ".m4v": true, ".mkv": true, ".webm": true, ".mov": true,
	".avi": true, ".wmv": true, ".asf": true, ".flv": true, ".ts": true,
	".m2ts": true, ".mts": true, ".mpg": true, ".mpeg": true, ".3gp": true,
	".3g2": true, ".ogv": true, ".vob": true,
}

var audioExts = map[string]bool{
	".mp3": true, ".flac": true, ".wav": true, ".ogg": true, ".opus": true,
	".m4a": true, ".aac": true, ".wma": true, ".aiff": true, ".aif": true,
	".mid": true, ".midi": true, ".ape": true, ".alac": true, ".wv": true,
}

// IsVideoExt reports whether ext (case-insensitive, with leading dot) is a supported video extension.
func IsVideoExt(ext string) bool { return videoExts[strings.ToLower(ext)] }

// IsAudioExt reports whether ext (case-insensitive, with leading dot) is a supported audio extension.
func IsAudioExt(ext string) bool { return audioExts[strings.ToLower(ext)] }

// TrackMetadata holds parsed audio/video metadata and embedded cover art.
type TrackMetadata struct {
	Title      string
	Artist     string
	Album      string
	Year       string
	Format     string
	SampleRate int
	Channels   int
	BitDepth   int
	Duration   time.Duration
	CoverArt   []byte
}

// LyricLine represents one timestamped line from a .lrc file.
type LyricLine struct {
	Time time.Duration
	Text string
}

// InspectMedia reads container headers and tags from path using pure-Go parsers.
func InspectMedia(path string) TrackMetadata {
	ext := strings.ToLower(filepath.Ext(path))
	meta := TrackMetadata{
		Format: strings.TrimPrefix(strings.ToUpper(ext), "."),
	}

	f, err := os.Open(path)
	if err != nil {
		return meta
	}
	defer f.Close()

	// Cap read to 8 MiB so huge movies never allocate excessively when scanning headers/cover art.
	head, err := io.ReadAll(io.LimitReader(f, 8<<20))
	if err != nil || len(head) < 4 {
		return meta
	}

	if bytes.HasPrefix(head, []byte("ID3")) {
		parseID3v2(head, &meta)
	}
	switch {
	case bytes.HasPrefix(head, []byte("fLaC")):
		parseFLAC(head, &meta)
	case len(head) >= 12 && bytes.HasPrefix(head, []byte("RIFF")) && bytes.Equal(head[8:12], []byte("WAVE")):
		parseWAV(head, &meta)
	case len(head) >= 12 && bytes.Equal(head[4:8], []byte("ftyp")):
		parseMP4Tags(head, &meta)
	}
	return meta
}

// LoadSidecarLRC looks for a .lrc file matching audioPath with the same stem.
func LoadSidecarLRC(audioPath string) []LyricLine {
	ext := filepath.Ext(audioPath)
	if ext == "" {
		return nil
	}
	lrcPath := strings.TrimSuffix(audioPath, ext) + ".lrc"
	raw, err := os.ReadFile(lrcPath)
	if err != nil {
		return nil
	}
	return ParseLRC(string(raw))
}

// ParseLRC parses LRC synced lyric timestamps into chronological order.
func ParseLRC(raw string) []LyricLine {
	raw = strings.TrimPrefix(raw, "\xEF\xBB\xBF")
	var out []LyricLine
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		var times []time.Duration
		rest := line
		for strings.HasPrefix(rest, "[") {
			closeIdx := strings.IndexByte(rest, ']')
			if closeIdx <= 1 {
				break
			}
			tag := rest[1:closeIdx]
			if d, ok := parseLRCTag(tag); ok {
				times = append(times, d)
				rest = rest[closeIdx+1:]
				continue
			}
			break
		}
		if len(times) == 0 {
			continue
		}
		text := strings.TrimSpace(rest)
		for _, t := range times {
			out = append(out, LyricLine{Time: t, Text: text})
		}
	}
	slices.SortStableFunc(out, func(a, b LyricLine) int {
		if a.Time < b.Time {
			return -1
		}
		if a.Time > b.Time {
			return 1
		}
		return 0
	})
	return out
}

// ActiveLyricIndex returns the index of the latest lyric line <= pos, or -1 if none.
func ActiveLyricIndex(lines []LyricLine, pos time.Duration) int {
	active := -1
	for i, l := range lines {
		if l.Time <= pos {
			active = i
		} else {
			break
		}
	}
	return active
}

func parseLRCTag(tag string) (time.Duration, bool) {
	colon := strings.IndexByte(tag, ':')
	if colon <= 0 {
		return 0, false
	}
	mins, err := strconv.Atoi(tag[:colon])
	if err != nil || mins < 0 {
		return 0, false
	}
	secPart := tag[colon+1:]
	var secs, millis int
	if dot := strings.IndexByte(secPart, '.'); dot >= 0 {
		secs, err = strconv.Atoi(secPart[:dot])
		if err != nil || secs < 0 {
			return 0, false
		}
		frac := secPart[dot+1:]
		if len(frac) > 3 {
			frac = frac[:3]
		}
		for len(frac) < 3 {
			frac += "0"
		}
		millis, err = strconv.Atoi(frac)
		if err != nil || millis < 0 {
			return 0, false
		}
	} else {
		secs, err = strconv.Atoi(secPart)
		if err != nil || secs < 0 {
			return 0, false
		}
	}
	return time.Duration(mins)*time.Minute + time.Duration(secs)*time.Second + time.Duration(millis)*time.Millisecond, true
}

func parseID3v2(data []byte, meta *TrackMetadata) {
	if len(data) < 10 || !bytes.HasPrefix(data, []byte("ID3")) {
		return
	}
	ver := data[3]
	if ver < 3 || ver > 4 {
		return
	}
	flags := data[5]
	tagSize := int(readSyncsafeUint32(data[6:10]))
	end := min(len(data), 10+tagSize)
	pos := 10
	if flags&0x40 != 0 && pos+4 <= end {
		extSize := int(readSyncsafeUint32(data[pos : pos+4]))
		if ver == 3 {
			extSize = int(binary.BigEndian.Uint32(data[pos:pos+4])) + 4
		}
		pos += max(0, extSize)
	}

	for pos+10 <= end {
		if data[pos] == 0 {
			break
		}
		id := string(data[pos : pos+4])
		var frameSize int
		if ver == 4 {
			frameSize = int(readSyncsafeUint32(data[pos+4 : pos+8]))
		} else {
			frameSize = int(binary.BigEndian.Uint32(data[pos+4 : pos+8]))
		}
		pos += 10
		if frameSize <= 0 || pos+frameSize > end {
			break
		}
		payload := data[pos : pos+frameSize]
		pos += frameSize

		switch id {
		case "TIT2":
			if s := decodeID3Text(payload); s != "" {
				meta.Title = s
			}
		case "TPE1":
			if s := decodeID3Text(payload); s != "" {
				meta.Artist = s
			}
		case "TALB":
			if s := decodeID3Text(payload); s != "" {
				meta.Album = s
			}
		case "TYER", "TDRC":
			if s := decodeID3Text(payload); s != "" && meta.Year == "" {
				meta.Year = s
			}
		case "APIC":
			if len(meta.CoverArt) == 0 {
				if img := extractID3Picture(payload); len(img) > 0 {
					meta.CoverArt = img
				}
			}
		}
	}
}

func readSyncsafeUint32(b []byte) uint32 {
	if len(b) < 4 {
		return 0
	}
	return (uint32(b[0]&0x7F) << 21) |
		(uint32(b[1]&0x7F) << 14) |
		(uint32(b[2]&0x7F) << 7) |
		uint32(b[3]&0x7F)
}

func decodeID3Text(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	enc := payload[0]
	raw := payload[1:]
	var s string
	switch enc {
	case 0: // ISO-8859-1
		buf := make([]rune, len(raw))
		for i, b := range raw {
			buf[i] = rune(b)
		}
		s = string(buf)
	case 1, 2: // UTF-16 BOM / UTF-16BE
		le := false
		if enc == 1 && len(raw) >= 2 {
			if raw[0] == 0xFF && raw[1] == 0xFE {
				le = true
				raw = raw[2:]
			} else if raw[0] == 0xFE && raw[1] == 0xFF {
				raw = raw[2:]
			}
		}
		u := make([]uint16, 0, len(raw)/2)
		for i := 0; i+1 < len(raw); i += 2 {
			var v uint16
			if le {
				v = binary.LittleEndian.Uint16(raw[i : i+2])
			} else {
				v = binary.BigEndian.Uint16(raw[i : i+2])
			}
			if v == 0 {
				break
			}
			u = append(u, v)
		}
		s = string(utf16.Decode(u))
	case 3: // UTF-8
		s = string(raw)
	default:
		if utf8.Valid(raw) {
			s = string(raw)
		}
	}
	return strings.TrimSpace(strings.TrimRight(s, "\x00"))
}

func extractID3Picture(payload []byte) []byte {
	if len(payload) < 4 {
		return nil
	}
	enc := payload[0]
	pos := 1
	// Skip MIME type null-terminated ASCII string.
	nul := bytes.IndexByte(payload[pos:], 0)
	if nul < 0 {
		return nil
	}
	pos += nul + 1
	if pos >= len(payload) {
		return nil
	}
	// Skip picture type byte.
	pos++
	if pos >= len(payload) {
		return nil
	}
	// Skip description string.
	if enc == 1 || enc == 2 {
		for pos+1 < len(payload) {
			if payload[pos] == 0 && payload[pos+1] == 0 {
				pos += 2
				break
			}
			pos += 2
		}
	} else {
		nul = bytes.IndexByte(payload[pos:], 0)
		if nul < 0 {
			return nil
		}
		pos += nul + 1
	}
	if pos >= len(payload) {
		return nil
	}
	img := make([]byte, len(payload)-pos)
	copy(img, payload[pos:])
	return img
}

func parseFLAC(data []byte, meta *TrackMetadata) {
	if len(data) < 8 || !bytes.HasPrefix(data, []byte("fLaC")) {
		return
	}
	pos := 4
	for pos+4 <= len(data) {
		hdr := data[pos]
		isLast := hdr&0x80 != 0
		blockType := hdr & 0x7F
		size := (int(data[pos+1]) << 16) | (int(data[pos+2]) << 8) | int(data[pos+3])
		pos += 4
		if pos+size > len(data) {
			break
		}
		block := data[pos : pos+size]
		pos += size

		switch blockType {
		case 0: // STREAMINFO
			if len(block) >= 18 {
				sr := (int(block[10]) << 12) | (int(block[11]) << 4) | (int(block[12]) >> 4)
				ch := ((int(block[12]) >> 1) & 0x07) + 1
				bps := (((int(block[12]) & 0x01) << 4) | (int(block[13]) >> 4)) + 1
				samples := (uint64(block[13]&0x0F) << 32) | uint64(binary.BigEndian.Uint32(block[14:18]))
				meta.SampleRate = sr
				meta.Channels = ch
				meta.BitDepth = bps
				if sr > 0 && samples > 0 {
					meta.Duration = time.Duration(samples) * time.Second / time.Duration(sr)
				}
			}
		case 4: // VORBIS_COMMENT
			parseVorbisComments(block, meta)
		case 6: // PICTURE
			if len(meta.CoverArt) == 0 && len(block) >= 32 {
				if img := extractFLACPicture(block); len(img) > 0 {
					meta.CoverArt = img
				}
			}
		}
		if isLast {
			break
		}
	}
}

func parseVorbisComments(block []byte, meta *TrackMetadata) {
	if len(block) < 8 {
		return
	}
	vLen := int(binary.LittleEndian.Uint32(block[0:4]))
	pos := 4 + vLen
	if pos < 4 || pos+4 > len(block) {
		return
	}
	count := int(binary.LittleEndian.Uint32(block[pos : pos+4]))
	pos += 4
	for i := 0; i < count && pos+4 <= len(block); i++ {
		l := int(binary.LittleEndian.Uint32(block[pos : pos+4]))
		pos += 4
		if l < 0 || pos+l > len(block) {
			break
		}
		entry := string(block[pos : pos+l])
		pos += l
		if eq := strings.IndexByte(entry, '='); eq > 0 {
			k := strings.ToUpper(strings.TrimSpace(entry[:eq]))
			v := strings.TrimSpace(entry[eq+1:])
			switch k {
			case "TITLE":
				meta.Title = v
			case "ARTIST":
				meta.Artist = v
			case "ALBUM":
				meta.Album = v
			case "DATE", "YEAR":
				if meta.Year == "" {
					meta.Year = v
				}
			}
		}
	}
}

func extractFLACPicture(block []byte) []byte {
	pos := 4 // skip picture type
	if pos+4 > len(block) {
		return nil
	}
	mimeLen := int(binary.BigEndian.Uint32(block[pos : pos+4]))
	pos += 4 + mimeLen
	if pos+4 > len(block) || mimeLen < 0 {
		return nil
	}
	descLen := int(binary.BigEndian.Uint32(block[pos : pos+4]))
	pos += 4 + descLen + 16 // skip w, h, depth, colors
	if pos+4 > len(block) || descLen < 0 {
		return nil
	}
	imgLen := int(binary.BigEndian.Uint32(block[pos : pos+4]))
	pos += 4
	if imgLen <= 0 || pos+imgLen > len(block) {
		return nil
	}
	out := make([]byte, imgLen)
	copy(out, block[pos:pos+imgLen])
	return out
}

func parseWAV(data []byte, meta *TrackMetadata) {
	if len(data) < 12 {
		return
	}
	pos := 12
	var byteRate uint32
	var dataSize uint32
	for pos+8 <= len(data) {
		chunkID := string(data[pos : pos+4])
		chunkSize := binary.LittleEndian.Uint32(data[pos+4 : pos+8])
		pos += 8
		if pos+int(chunkSize) > len(data) {
			break
		}
		chunk := data[pos : pos+int(chunkSize)]
		switch chunkID {
		case "fmt ":
			if len(chunk) >= 16 {
				meta.Channels = int(binary.LittleEndian.Uint16(chunk[2:4]))
				meta.SampleRate = int(binary.LittleEndian.Uint32(chunk[4:8]))
				byteRate = binary.LittleEndian.Uint32(chunk[8:12])
				meta.BitDepth = int(binary.LittleEndian.Uint16(chunk[14:16]))
			}
		case "data":
			dataSize = chunkSize
		}
		pos += int(chunkSize)
		if chunkSize&1 == 1 {
			pos++
		}
	}
	if byteRate > 0 && dataSize > 0 {
		meta.Duration = time.Duration(float64(dataSize) / float64(byteRate) * float64(time.Second))
	}
}

func parseMP4Tags(data []byte, meta *TrackMetadata) {
	walkAtoms(data, func(atomType string, body []byte) {
		switch atomType {
		case "moov", "udta", "ilst":
			parseMP4Tags(body, meta)
		case "meta":
			if len(body) > 4 {
				parseMP4Tags(body[4:], meta)
			}
		case "\xA9nam", "\xA9ART", "\xA9alb", "\xA9day", "covr":
			val := extractMP4DataAtom(body)
			if len(val) == 0 {
				return
			}
			switch atomType {
			case "\xA9nam":
				meta.Title = strings.TrimSpace(string(val))
			case "\xA9ART":
				meta.Artist = strings.TrimSpace(string(val))
			case "\xA9alb":
				meta.Album = strings.TrimSpace(string(val))
			case "\xA9day":
				meta.Year = strings.TrimSpace(string(val))
			case "covr":
				if len(meta.CoverArt) == 0 {
					cp := make([]byte, len(val))
					copy(cp, val)
					meta.CoverArt = cp
				}
			}
		}
	})
}

func walkAtoms(buf []byte, fn func(atomType string, body []byte)) {
	pos := 0
	for pos+8 <= len(buf) {
		size := int(binary.BigEndian.Uint32(buf[pos : pos+4]))
		atomType := string(buf[pos+4 : pos+8])
		hdrLen := 8
		if size == 1 {
			if pos+16 > len(buf) {
				break
			}
			sz64 := binary.BigEndian.Uint64(buf[pos+8 : pos+16])
			if sz64 > uint64(len(buf)-pos) {
				break
			}
			size = int(sz64)
			hdrLen = 16
		} else if size == 0 {
			size = len(buf) - pos
		}
		if size < hdrLen || pos+size > len(buf) {
			break
		}
		fn(atomType, buf[pos+hdrLen:pos+size])
		pos += size
	}
}

func extractMP4DataAtom(buf []byte) []byte {
	var out []byte
	walkAtoms(buf, func(atomType string, body []byte) {
		if atomType == "data" && len(body) > 8 {
			out = body[8:]
		}
	})
	return out
}

// FormatClock renders d as mm:ss or hh:mm:ss.
func FormatClock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	secs := int(d.Round(time.Second).Seconds())
	h := secs / 3600
	m := (secs % 3600) / 60
	s := secs % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}
