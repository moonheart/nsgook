// Package imageviewer implements NSGook's image, animated GIF, SVG, and RAW/bitmap previewer.
package imageviewer

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

// ImageMeta summarizes dimensions, format, and optional EXIF metadata for an image file.
type ImageMeta struct {
	Width       int
	Height      int
	Format      string
	Animated    bool
	CameraMake  string
	CameraModel string
	DateTime    string
	Orientation int
}

var wellKnownExts = map[string]bool{
	".apng": true, ".bmp": true, ".cur": true, ".gif": true, ".ico": true,
	".icon": true, ".jfif": true, ".jpeg": true, ".jpg": true, ".pbm": true,
	".pgm": true, ".png": true, ".pnm": true, ".ppm": true, ".psb": true,
	".psd": true, ".qoi": true, ".svg": true, ".svgz": true, ".tga": true,
	".tif": true, ".tiff": true, ".webp": true,
}

func isSupportedExt(ext string) bool {
	return wellKnownExts[strings.ToLower(ext)]
}

func hasImageMagic(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var buf [12]byte
	n, err := io.ReadFull(f, buf[:])
	if err != nil && err != io.ErrUnexpectedEOF {
		return false
	}
	b := buf[:n]
	switch {
	case len(b) >= 8 && bytes.Equal(b[:8], []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}):
		return true
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return true
	case len(b) >= 6 && (string(b[:6]) == "GIF87a" || string(b[:6]) == "GIF89a"):
		return true
	case len(b) >= 2 && b[0] == 'B' && b[1] == 'M':
		return true
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return true
	case len(b) >= 4 && string(b[:4]) == "qoif":
		return true
	case len(b) >= 4 && string(b[:4]) == "8BPS":
		return true
	}
	return false
}

// ReadMeta quickly extracts dimensions and metadata without decoding the full pixel buffer when possible.
func ReadMeta(path string) ImageMeta {
	ext := strings.ToLower(filepath.Ext(path))
	meta := ImageMeta{Format: strings.TrimPrefix(strings.ToUpper(ext), ".")}
	f, err := os.Open(path)
	if err != nil {
		return meta
	}
	defer f.Close()

	switch ext {
	case ".svg", ".svgz":
		data, err := readSVGData(path)
		if err == nil {
			w, h := parseSVGDimensions(data)
			meta.Width, meta.Height = w, h
			meta.Format = "SVG"
		}
		return meta
	case ".qoi":
		var hdr [14]byte
		if _, err := io.ReadFull(f, hdr[:]); err == nil && string(hdr[:4]) == "qoif" {
			meta.Width = int(binary.BigEndian.Uint32(hdr[4:8]))
			meta.Height = int(binary.BigEndian.Uint32(hdr[8:12]))
			meta.Format = "QOI"
		}
		return meta
	case ".psd", ".psb":
		var hdr [26]byte
		if _, err := io.ReadFull(f, hdr[:]); err == nil && string(hdr[:4]) == "8BPS" {
			meta.Height = int(binary.BigEndian.Uint32(hdr[14:18]))
			meta.Width = int(binary.BigEndian.Uint32(hdr[18:22]))
			meta.Format = "PSD"
		}
		return meta
	case ".tga":
		var hdr [18]byte
		if _, err := io.ReadFull(f, hdr[:]); err == nil {
			meta.Width = int(binary.LittleEndian.Uint16(hdr[12:14]))
			meta.Height = int(binary.LittleEndian.Uint16(hdr[14:16]))
			meta.Format = "TGA"
		}
		return meta
	}

	cfg, format, err := image.DecodeConfig(f)
	if err == nil {
		meta.Width = cfg.Width
		meta.Height = cfg.Height
		if format != "" {
			meta.Format = strings.ToUpper(format)
		}
	}
	if ext == ".gif" || format == "gif" {
		_, _ = f.Seek(0, io.SeekStart)
		meta.Animated = isAnimatedGIF(f)
	}
	if ext == ".jpg" || ext == ".jpeg" || ext == ".jfif" || format == "jpeg" {
		_, _ = f.Seek(0, io.SeekStart)
		readBasicEXIF(f, &meta)
	}
	return meta
}

const (
	maxNativeURIPixels     = 1 << 26 // Matches nsgo imagedecode.MaxBitmapPixels (64M px).
	maxNativeAnimURIPixels = 1 << 24 // Matches nsgo imagedecode.MaxPixels (16M px) for animations.
)

// CanNativeURI reports whether NSGo's native image pipeline can load and decode meta directly from raw
// compressed bytes (ImageSource.Data) without allocating app-side RGBA/pixelFrame copies or pinning imageload.cache.
func CanNativeURI(meta ImageMeta) bool {
	if meta.Width <= 0 || meta.Height <= 0 || meta.Orientation > 1 {
		return false
	}
	pixels := int64(meta.Width) * int64(meta.Height)
	if pixels > maxNativeURIPixels {
		return false
	}
	if meta.Animated && pixels > maxNativeAnimURIPixels {
		return false
	}
	switch strings.ToUpper(meta.Format) {
	case "PNG", "JPEG", "GIF", "WEBP":
		return true
	default:
		return false
	}
}

// FileURI converts a local or UNC filesystem path into a percent-encoded file:// URI compatible with NSGo's imageload,
// encoding a cache-busting query token from modTimeUnix and fileSize so modified files bypass stale cache entries.
func FileURI(path string, modTimeUnix int64, fileSize int64) string {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		if abs, err := filepath.Abs(clean); err == nil {
			clean = abs
		}
	}
	slashed := filepath.ToSlash(clean)
	var u url.URL
	u.Scheme = "file"
	if strings.HasPrefix(slashed, "//") {
		rest := strings.TrimPrefix(slashed, "//")
		host, p, ok := strings.Cut(rest, "/")
		if !ok || host == "" {
			return clean
		}
		u.Host = host
		u.Path = "/" + p
	} else {
		if !strings.HasPrefix(slashed, "/") {
			slashed = "/" + slashed
		}
		u.Path = slashed
	}
	if modTimeUnix != 0 || fileSize != 0 {
		u.RawQuery = fmt.Sprintf("v=%d_%d", modTimeUnix, fileSize)
	}
	return u.String()
}

// isAnimatedGIF inspects GIF block headers without decompressing LZW pixel data and reports whether >1 frame exists.
func isAnimatedGIF(r io.Reader) bool {
	br := bufio.NewReaderSize(r, 4096)
	var hdr [13]byte
	if _, err := io.ReadFull(br, hdr[:]); err != nil {
		return false
	}
	sig := string(hdr[:6])
	if sig != "GIF87a" && sig != "GIF89a" {
		return false
	}
	packed := hdr[10]
	if packed&0x80 != 0 {
		gctBytes := 3 * (1 << ((packed & 0x07) + 1))
		if _, err := br.Discard(gctBytes); err != nil {
			return false
		}
	}
	skipSubBlocks := func() bool {
		for {
			n, err := br.ReadByte()
			if err != nil {
				return false
			}
			if n == 0 {
				return true
			}
			if _, err := br.Discard(int(n)); err != nil {
				return false
			}
		}
	}
	frames := 0
	for {
		b, err := br.ReadByte()
		if err != nil {
			return false
		}
		switch b {
		case 0x3B: // Trailer
			return false
		case 0x21: // Extension block: 1-byte label + sub-blocks
			if _, err := br.ReadByte(); err != nil {
				return false
			}
			if !skipSubBlocks() {
				return false
			}
		case 0x2C: // Image descriptor
			frames++
			if frames > 1 {
				return true
			}
			var desc [9]byte
			if _, err := io.ReadFull(br, desc[:]); err != nil {
				return false
			}
			if desc[8]&0x80 != 0 {
				lctBytes := 3 * (1 << ((desc[8] & 0x07) + 1))
				if _, err := br.Discard(lctBytes); err != nil {
					return false
				}
			}
			// LZW minimum code size byte + sub-blocks
			if _, err := br.ReadByte(); err != nil {
				return false
			}
			if !skipSubBlocks() {
				return false
			}
		default:
			return false
		}
	}
}

// DecodedImage holds the decoded image representation ready for NSGo rendering.
type DecodedImage struct {
	RGBA     *image.RGBA
	RawData  []byte // Set for animated GIF or SVG bytes passed directly to core.Image
	IsSVG    bool
	Animated bool
	Meta     ImageMeta
}

// DecodeFile reads and decodes path into RGBA pixels or raw SVG/animated GIF bytes.
func DecodeFile(path string) (*DecodedImage, error) {
	ext := strings.ToLower(filepath.Ext(path))
	meta := ReadMeta(path)

	switch ext {
	case ".png", ".jpg", ".jpeg", ".jfif", ".bmp", ".tif", ".tiff", ".webp":
		if f, err := os.Open(path); err == nil {
			img, _, decErr := image.Decode(bufio.NewReaderSize(f, 32*1024))
			_ = f.Close()
			if decErr == nil {
				rgba := ToRGBA(img)
				if meta.Orientation >= 2 && meta.Orientation <= 8 {
					rgba = applyExifOrientation(rgba, meta.Orientation)
				}
				meta.Width = rgba.Bounds().Dx()
				meta.Height = rgba.Bounds().Dy()
				return &DecodedImage{RGBA: rgba, Meta: meta}, nil
			}
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if ext == ".svg" || ext == ".svgz" {
		svgBytes := data
		if ext == ".svgz" {
			if dec, gErr := gunzipBytes(data); gErr == nil {
				svgBytes = dec
			}
		}
		if meta.Width <= 0 || meta.Height <= 0 {
			meta.Width, meta.Height = parseSVGDimensions(svgBytes)
		}
		if meta.Width <= 0 || meta.Height <= 0 {
			meta.Width, meta.Height = 640, 480
		}
		meta.Format = "SVG"
		return &DecodedImage{RawData: svgBytes, IsSVG: true, Meta: meta}, nil
	}

	if ext == ".gif" || (len(data) >= 6 && (string(data[:6]) == "GIF87a" || string(data[:6]) == "GIF89a")) {
		if g, gErr := gif.DecodeAll(bytes.NewReader(data)); gErr == nil {
			if len(g.Image) > 1 {
				meta.Animated = true
				meta.Width = g.Config.Width
				meta.Height = g.Config.Height
				return &DecodedImage{
					RGBA:     ToRGBA(g.Image[0]),
					RawData:  data,
					Animated: true,
					Meta:     meta,
				}, nil
			}
		}
	}

	rgba, err := DecodeBytes(data, ext)
	if err != nil {
		return nil, err
	}
	if meta.Orientation >= 2 && meta.Orientation <= 8 {
		rgba = applyExifOrientation(rgba, meta.Orientation)
	}
	meta.Width = rgba.Bounds().Dx()
	meta.Height = rgba.Bounds().Dy()
	return &DecodedImage{RGBA: rgba, Meta: meta}, nil
}

// DecodeBytes decodes encoded image bytes across stdlib, x/image, and custom pure-Go decoders.
func DecodeBytes(data []byte, ext string) (*image.RGBA, error) {
	switch strings.ToLower(ext) {
	case ".qoi":
		return DecodeQOI(data)
	case ".ico", ".cur", ".icon":
		return DecodeICO(data)
	case ".tga":
		return DecodeTGA(data)
	case ".ppm", ".pgm", ".pbm", ".pnm":
		return DecodeNetpbm(data)
	case ".psd", ".psb":
		return DecodePSD(data)
	}

	if img, _, err := image.Decode(bytes.NewReader(data)); err == nil {
		return ToRGBA(img), nil
	}

	// Magic fallback for extensionless files
	switch {
	case len(data) >= 4 && string(data[:4]) == "qoif":
		return DecodeQOI(data)
	case len(data) >= 4 && string(data[:4]) == "8BPS":
		return DecodePSD(data)
	}
	return nil, errors.New("unsupported or corrupt image format")
}

// ToRGBA converts any image.Image into a tight *image.RGBA with Min at (0,0).
func ToRGBA(src image.Image) *image.RGBA {
	if r, ok := src.(*image.RGBA); ok && r.Bounds().Min == (image.Point{}) && r.Stride == 4*r.Bounds().Dx() {
		return r
	}
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	return dst
}

// Rotate90 rotates src clockwise by turns*90 degrees.
func Rotate90(src *image.RGBA, turns int) *image.RGBA {
	if src == nil {
		return nil
	}
	turns = ((turns % 4) + 4) % 4
	if turns == 0 {
		return src
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	var dst *image.RGBA
	switch turns {
	case 1: // 90 CW
		dst = image.NewRGBA(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				so := y*src.Stride + x*4
				do := x*dst.Stride + (h-1-y)*4
				copy(dst.Pix[do:do+4], src.Pix[so:so+4])
			}
		}
	case 2: // 180
		dst = image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				so := y*src.Stride + x*4
				do := (h-1-y)*dst.Stride + (w-1-x)*4
				copy(dst.Pix[do:do+4], src.Pix[so:so+4])
			}
		}
	case 3: // 270 CW
		dst = image.NewRGBA(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				so := y*src.Stride + x*4
				do := (w-1-x)*dst.Stride + y*4
				copy(dst.Pix[do:do+4], src.Pix[so:so+4])
			}
		}
	}
	return dst
}

// Flip mirrors src horizontally and/or vertically.
func Flip(src *image.RGBA, horiz, vert bool) *image.RGBA {
	if src == nil || (!horiz && !vert) {
		return src
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		dy := y
		if vert {
			dy = h - 1 - y
		}
		for x := 0; x < w; x++ {
			dx := x
			if horiz {
				dx = w - 1 - x
			}
			so := y*src.Stride + x*4
			do := dy*dst.Stride + dx*4
			copy(dst.Pix[do:do+4], src.Pix[so:so+4])
		}
	}
	return dst
}

func applyExifOrientation(src *image.RGBA, orient int) *image.RGBA {
	switch orient {
	case 2:
		return Flip(src, true, false)
	case 3:
		return Rotate90(src, 2)
	case 4:
		return Flip(src, false, true)
	case 5:
		return Flip(Rotate90(src, 1), true, false)
	case 6:
		return Rotate90(src, 1)
	case 7:
		return Flip(Rotate90(src, 3), true, false)
	case 8:
		return Rotate90(src, 3)
	default:
		return src
	}
}

// DecodeQOI decodes Quite OK Image (.qoi) data.
func DecodeQOI(data []byte) (*image.RGBA, error) {
	if len(data) < 22 || string(data[:4]) != "qoif" {
		return nil, errors.New("invalid QOI header")
	}
	w := int(binary.BigEndian.Uint32(data[4:8]))
	h := int(binary.BigEndian.Uint32(data[8:12]))
	channels := data[12]
	if w <= 0 || h <= 0 || w > 32768 || h > 32768 || (channels != 3 && channels != 4) {
		return nil, errors.New("unsupported QOI dimensions")
	}

	total := w * h
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	var index [64]color.NRGBA
	px := color.NRGBA{R: 0, G: 0, B: 0, A: 255}
	pos := 14
	end := len(data) - 8
	run := 0

	for i := 0; i < total; i++ {
		if run > 0 {
			run--
		} else if pos < end {
			b1 := data[pos]
			pos++
			switch {
			case b1 == 0xFE && pos+3 <= end:
				px.R, px.G, px.B = data[pos], data[pos+1], data[pos+2]
				pos += 3
			case b1 == 0xFF && pos+4 <= end:
				px.R, px.G, px.B, px.A = data[pos], data[pos+1], data[pos+2], data[pos+3]
				pos += 4
			case b1&0xC0 == 0x00:
				px = index[b1&0x3F]
			case b1&0xC0 == 0x40:
				px.R += ((b1 >> 4) & 0x03) - 2
				px.G += ((b1 >> 2) & 0x03) - 2
				px.B += (b1 & 0x03) - 2
			case b1&0xC0 == 0x80 && pos < end:
				b2 := data[pos]
				pos++
				vg := int(b1&0x3F) - 32
				px.R += uint8(vg + int((b2>>4)&0x0F) - 8)
				px.G += uint8(vg)
				px.B += uint8(vg + int(b2&0x0F) - 8)
			case b1&0xC0 == 0xC0:
				run = int(b1 & 0x3F)
			}
			hash := (int(px.R)*3 + int(px.G)*5 + int(px.B)*7 + int(px.A)*11) & 63
			index[hash] = px
		}
		off := i * 4
		out.Pix[off] = px.R
		out.Pix[off+1] = px.G
		out.Pix[off+2] = px.B
		out.Pix[off+3] = px.A
	}
	return ToRGBA(out), nil
}

// DecodeICO decodes Windows .ico / .cur containers, selecting the largest frame.
func DecodeICO(data []byte) (*image.RGBA, error) {
	if len(data) < 6 {
		return nil, errors.New("invalid ICO header")
	}
	resType := binary.LittleEndian.Uint16(data[2:4])
	count := int(binary.LittleEndian.Uint16(data[4:6]))
	if (resType != 1 && resType != 2) || count <= 0 || len(data) < 6+count*16 {
		return nil, errors.New("invalid ICO directory")
	}

	bestIdx := -1
	bestArea := -1
	for i := 0; i < count; i++ {
		off := 6 + i*16
		w := int(data[off])
		if w == 0 {
			w = 256
		}
		h := int(data[off+1])
		if h == 0 {
			h = 256
		}
		size := int(binary.LittleEndian.Uint32(data[off+8 : off+12]))
		imgOff := int(binary.LittleEndian.Uint32(data[off+12 : off+16]))
		if imgOff+size > len(data) || size <= 0 {
			continue
		}
		if w*h > bestArea {
			bestArea = w * h
			bestIdx = i
		}
	}
	if bestIdx < 0 {
		return nil, errors.New("no valid icon frames")
	}
	off := 6 + bestIdx*16
	size := int(binary.LittleEndian.Uint32(data[off+8 : off+12]))
	imgOff := int(binary.LittleEndian.Uint32(data[off+12 : off+16]))
	frame := data[imgOff : imgOff+size]

	if len(frame) >= 8 && bytes.Equal(frame[:8], []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}) {
		img, _, err := image.Decode(bytes.NewReader(frame))
		if err != nil {
			return nil, err
		}
		return ToRGBA(img), nil
	}
	return decodeICODIB(frame)
}

func decodeICODIB(dib []byte) (*image.RGBA, error) {
	if len(dib) < 40 {
		return nil, errors.New("invalid ICO DIB")
	}
	hdrSize := int(binary.LittleEndian.Uint32(dib[0:4]))
	w := int(int32(binary.LittleEndian.Uint32(dib[4:8])))
	hTotal := int(int32(binary.LittleEndian.Uint32(dib[8:12])))
	h := hTotal / 2
	bpp := int(binary.LittleEndian.Uint16(dib[14:16]))
	if hdrSize < 40 || w <= 0 || h <= 0 || hdrSize >= len(dib) {
		return nil, errors.New("unsupported ICO DIB header")
	}
	if bpp != 32 && bpp != 24 {
		return nil, fmt.Errorf("unsupported ICO bit depth: %d", bpp)
	}

	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	rowStride := ((w*bpp + 31) / 32) * 4
	pixOff := hdrSize
	if pixOff+rowStride*h > len(dib) {
		return nil, errors.New("truncated ICO DIB pixels")
	}
	andStride := ((w + 31) / 32) * 4
	andOff := pixOff + rowStride*h
	hasAnd := andOff+andStride*h <= len(dib)

	for y := 0; y < h; y++ {
		srcY := h - 1 - y
		row := dib[pixOff+srcY*rowStride : pixOff+(srcY+1)*rowStride]
		for x := 0; x < w; x++ {
			b, g, r := row[x*(bpp/8)], row[x*(bpp/8)+1], row[x*(bpp/8)+2]
			a := byte(255)
			if bpp == 32 {
				a = row[x*4+3]
			} else if hasAnd {
				maskByte := dib[andOff+srcY*andStride+(x/8)]
				if (maskByte>>(7-(x%8)))&1 != 0 {
					a = 0
				}
			}
			out.SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: b, A: a})
		}
	}
	return ToRGBA(out), nil
}

// DecodeTGA decodes Truevision TGA (24/32-bit RGB and 8-bit grayscale, uncompressed or RLE).
func DecodeTGA(data []byte) (*image.RGBA, error) {
	if len(data) < 18 {
		return nil, errors.New("invalid TGA header")
	}
	idLen := int(data[0])
	imgType := data[2]
	w := int(binary.LittleEndian.Uint16(data[12:14]))
	h := int(binary.LittleEndian.Uint16(data[14:16]))
	bpp := int(data[16])
	desc := data[17]
	if w <= 0 || h <= 0 || w > 16384 || h > 16384 {
		return nil, errors.New("invalid TGA dimensions")
	}
	if imgType != 2 && imgType != 3 && imgType != 10 && imgType != 11 {
		return nil, fmt.Errorf("unsupported TGA type %d", imgType)
	}
	bytesPerPx := bpp / 8
	if bytesPerPx != 1 && bytesPerPx != 3 && bytesPerPx != 4 {
		return nil, fmt.Errorf("unsupported TGA bpp %d", bpp)
	}
	pos := 18 + idLen
	if pos > len(data) {
		return nil, errors.New("truncated TGA data")
	}

	pixels := make([]byte, w*h*4)
	rle := imgType == 10 || imgType == 11
	readPx := func() (r, g, b, a byte, ok bool) {
		if pos+bytesPerPx > len(data) {
			return 0, 0, 0, 0, false
		}
		switch bytesPerPx {
		case 1:
			v := data[pos]
			pos++
			return v, v, v, 255, true
		case 3:
			b, g, r = data[pos], data[pos+1], data[pos+2]
			pos += 3
			return r, g, b, 255, true
		case 4:
			b, g, r, a = data[pos], data[pos+1], data[pos+2], data[pos+3]
			pos += 4
			return r, g, b, a, true
		}
		return 0, 0, 0, 0, false
	}

	total := w * h
	if !rle {
		for i := 0; i < total; i++ {
			r, g, b, a, ok := readPx()
			if !ok {
				break
			}
			pixels[i*4], pixels[i*4+1], pixels[i*4+2], pixels[i*4+3] = r, g, b, a
		}
	} else {
		i := 0
		for i < total && pos < len(data) {
			hdr := data[pos]
			pos++
			cnt := int(hdr&0x7F) + 1
			if hdr&0x80 != 0 {
				r, g, b, a, ok := readPx()
				if !ok {
					break
				}
				for j := 0; j < cnt && i < total; j++ {
					pixels[i*4], pixels[i*4+1], pixels[i*4+2], pixels[i*4+3] = r, g, b, a
					i++
				}
			} else {
				for j := 0; j < cnt && i < total; j++ {
					r, g, b, a, ok := readPx()
					if !ok {
						break
					}
					pixels[i*4], pixels[i*4+1], pixels[i*4+2], pixels[i*4+3] = r, g, b, a
					i++
				}
			}
		}
	}

	topToBottom := (desc & 0x20) != 0
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		dstY := y
		if !topToBottom {
			dstY = h - 1 - y
		}
		srcStart := y * w * 4
		dstStart := dstY * out.Stride
		copy(out.Pix[dstStart:dstStart+w*4], pixels[srcStart:srcStart+w*4])
	}
	return ToRGBA(out), nil
}

// DecodeNetpbm decodes P1..P6 Netpbm images (.pbm, .pgm, .ppm).
func DecodeNetpbm(data []byte) (*image.RGBA, error) {
	rd := bufio.NewReader(bytes.NewReader(data))
	readToken := func() (string, error) {
		var tok []byte
		for {
			b, err := rd.ReadByte()
			if err != nil {
				if len(tok) > 0 {
					return string(tok), nil
				}
				return "", err
			}
			if b == '#' {
				for {
					c, err := rd.ReadByte()
					if err != nil || c == '\n' {
						break
					}
				}
				if len(tok) > 0 {
					return string(tok), nil
				}
				continue
			}
			if b == ' ' || b == '\t' || b == '\r' || b == '\n' {
				if len(tok) > 0 {
					return string(tok), nil
				}
				continue
			}
			tok = append(tok, b)
		}
	}

	magic, err := readToken()
	if err != nil || len(magic) != 2 || magic[0] != 'P' {
		return nil, errors.New("invalid Netpbm magic")
	}
	wStr, err := readToken()
	if err != nil {
		return nil, err
	}
	hStr, err := readToken()
	if err != nil {
		return nil, err
	}
	w, _ := strconv.Atoi(wStr)
	h, _ := strconv.Atoi(hStr)
	if w <= 0 || h <= 0 || w > 16384 || h > 16384 {
		return nil, errors.New("invalid Netpbm dimensions")
	}
	maxVal := 255
	if magic != "P1" && magic != "P4" {
		mStr, err := readToken()
		if err != nil {
			return nil, err
		}
		maxVal, _ = strconv.Atoi(mStr)
		if maxVal <= 0 {
			maxVal = 255
		}
	}
	scale := func(v int) uint8 {
		if maxVal == 255 {
			return uint8(v)
		}
		return uint8((v * 255) / maxVal)
	}

	out := image.NewRGBA(image.Rect(0, 0, w, h))
	switch magic {
	case "P6":
		buf := make([]byte, w*h*3)
		_, _ = io.ReadFull(rd, buf)
		for i := 0; i < w*h; i++ {
			out.Pix[i*4] = scale(int(buf[i*3]))
			out.Pix[i*4+1] = scale(int(buf[i*3+1]))
			out.Pix[i*4+2] = scale(int(buf[i*3+2]))
			out.Pix[i*4+3] = 255
		}
	case "P5":
		buf := make([]byte, w*h)
		_, _ = io.ReadFull(rd, buf)
		for i := 0; i < w*h; i++ {
			v := scale(int(buf[i]))
			out.Pix[i*4], out.Pix[i*4+1], out.Pix[i*4+2], out.Pix[i*4+3] = v, v, v, 255
		}
	case "P4":
		stride := (w + 7) / 8
		buf := make([]byte, stride*h)
		_, _ = io.ReadFull(rd, buf)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				bit := (buf[y*stride+x/8] >> (7 - (x % 8))) & 1
				v := uint8(255)
				if bit != 0 {
					v = 0
				}
				i := (y*w + x) * 4
				out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] = v, v, v, 255
			}
		}
	case "P3":
		for i := 0; i < w*h; i++ {
			rStr, _ := readToken()
			gStr, _ := readToken()
			bStr, _ := readToken()
			r, _ := strconv.Atoi(rStr)
			g, _ := strconv.Atoi(gStr)
			b, _ := strconv.Atoi(bStr)
			out.Pix[i*4], out.Pix[i*4+1], out.Pix[i*4+2], out.Pix[i*4+3] = scale(r), scale(g), scale(b), 255
		}
	case "P2", "P1":
		for i := 0; i < w*h; i++ {
			vStr, _ := readToken()
			v, _ := strconv.Atoi(vStr)
			px := scale(v)
			if magic == "P1" {
				if v != 0 {
					px = 0
				} else {
					px = 255
				}
			}
			out.Pix[i*4], out.Pix[i*4+1], out.Pix[i*4+2], out.Pix[i*4+3] = px, px, px, 255
		}
	default:
		return nil, fmt.Errorf("unsupported Netpbm format %s", magic)
	}
	return out, nil
}

// DecodePSD extracts the embedded JPEG preview or decodes the composite RGB/RGBA channels of a PSD file.
func DecodePSD(data []byte) (*image.RGBA, error) {
	if len(data) < 26 || string(data[:4]) != "8BPS" {
		return nil, errors.New("invalid PSD header")
	}
	version := binary.BigEndian.Uint16(data[4:6])
	if version != 1 {
		return nil, errors.New("PSB large document not supported for raw composite")
	}
	channels := int(binary.BigEndian.Uint16(data[12:14]))
	h := int(binary.BigEndian.Uint32(data[14:18]))
	w := int(binary.BigEndian.Uint32(data[18:22]))
	depth := int(binary.BigEndian.Uint16(data[22:24]))
	colorMode := int(binary.BigEndian.Uint16(data[24:26]))

	pos := 26
	if pos+4 > len(data) {
		return nil, errors.New("truncated PSD color mode")
	}
	colorLen := int(binary.BigEndian.Uint32(data[pos : pos+4]))
	pos += 4 + colorLen
	if pos+4 > len(data) {
		return nil, errors.New("truncated PSD resources")
	}
	resLen := int(binary.BigEndian.Uint32(data[pos : pos+4]))
	pos += 4
	resEnd := min(pos+resLen, len(data))

	// Look for 8BIM resource 1036 (Photoshop 5+ JPEG thumbnail) first.
	for cur := pos; cur+12 <= resEnd; {
		if string(data[cur:cur+4]) != "8BIM" {
			break
		}
		resID := binary.BigEndian.Uint16(data[cur+4 : cur+6])
		nameLen := int(data[cur+6])
		nameTotal := 1 + nameLen
		if nameTotal%2 != 0 {
			nameTotal++
		}
		sizeOff := cur + 6 + nameTotal
		if sizeOff+4 > resEnd {
			break
		}
		blkSize := int(binary.BigEndian.Uint32(data[sizeOff : sizeOff+4]))
		blkStart := sizeOff + 4
		if resID == 1036 && blkSize > 28 && blkStart+blkSize <= resEnd {
			jpegBytes := data[blkStart+28 : blkStart+blkSize]
			if img, _, err := image.Decode(bytes.NewReader(jpegBytes)); err == nil && w*h > 2048*2048 {
				return ToRGBA(img), nil
			}
		}
		if blkSize%2 != 0 {
			blkSize++
		}
		cur = blkStart + blkSize
	}

	// Skip resources and Layer & Mask section to reach Image Data section.
	pos = resEnd
	if pos+4 > len(data) {
		return nil, errors.New("truncated PSD layers")
	}
	layerLen := int(binary.BigEndian.Uint32(data[pos : pos+4]))
	pos += 4 + layerLen
	if pos+2 > len(data) {
		return nil, errors.New("truncated PSD image data")
	}
	if depth != 8 || (colorMode != 3 && colorMode != 1) || channels < 1 || w <= 0 || h <= 0 {
		return nil, errors.New("unsupported PSD color mode or bit depth")
	}

	compression := binary.BigEndian.Uint16(data[pos : pos+2])
	pos += 2
	usedChannels := min(channels, 4)
	planeSize := w * h
	planes := make([][]byte, usedChannels)

	switch compression {
	case 0: // Raw
		for c := 0; c < usedChannels; c++ {
			if pos+planeSize > len(data) {
				return nil, errors.New("truncated raw PSD plane")
			}
			planes[c] = data[pos : pos+planeSize]
			pos += planeSize
		}
	case 1: // RLE PackBits
		// Skip per-scanline byte counts table (2 bytes * h * channels)
		countsLen := 2 * h * channels
		if pos+countsLen > len(data) {
			return nil, errors.New("truncated PSD RLE counts")
		}
		countsOff := pos
		pos += countsLen
		for c := 0; c < channels; c++ {
			var dst []byte
			if c < usedChannels {
				dst = make([]byte, 0, planeSize)
			}
			for y := 0; y < h; y++ {
				lineBytes := int(binary.BigEndian.Uint16(data[countsOff : countsOff+2]))
				countsOff += 2
				if pos+lineBytes > len(data) {
					return nil, errors.New("truncated PSD RLE row")
				}
				if c < usedChannels {
					dst = decodePackBits(dst, data[pos:pos+lineBytes], w)
				}
				pos += lineBytes
			}
			if c < usedChannels {
				for len(dst) < planeSize {
					dst = append(dst, 0)
				}
				planes[c] = dst[:planeSize]
			}
		}
	default:
		return nil, fmt.Errorf("unsupported PSD compression %d", compression)
	}

	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < planeSize; i++ {
		var r, g, b, a byte = 0, 0, 0, 255
		if colorMode == 1 {
			r = planes[0][i]
			g, b = r, r
			if usedChannels >= 2 {
				a = planes[1][i]
			}
		} else {
			r = planes[0][i]
			if usedChannels >= 2 {
				g = planes[1][i]
			}
			if usedChannels >= 3 {
				b = planes[2][i]
			}
			if usedChannels >= 4 {
				a = planes[3][i]
			}
		}
		out.Pix[i*4], out.Pix[i*4+1], out.Pix[i*4+2], out.Pix[i*4+3] = r, g, b, a
	}
	return ToRGBA(out), nil
}

func decodePackBits(dst, src []byte, maxOut int) []byte {
	written := 0
	for i := 0; i < len(src) && written < maxOut; {
		n := int(int8(src[i]))
		i++
		if n >= 0 {
			cnt := min(n+1, len(src)-i, maxOut-written)
			dst = append(dst, src[i:i+cnt]...)
			i += n + 1
			written += cnt
		} else if n != -128 {
			if i >= len(src) {
				break
			}
			val := src[i]
			i++
			cnt := min(1-n, maxOut-written)
			for j := 0; j < cnt; j++ {
				dst = append(dst, val)
			}
			written += cnt
		}
	}
	for written < maxOut {
		dst = append(dst, 0)
		written++
	}
	return dst
}

func readSVGData(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(filepath.Ext(path), ".svgz") {
		if dec, gErr := gunzipBytes(data); gErr == nil {
			return dec, nil
		}
	}
	return data, nil
}

func gunzipBytes(data []byte) ([]byte, error) {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gr.Close()
	return io.ReadAll(io.LimitReader(gr, 32<<20))
}

func parseSVGDimensions(data []byte) (int, int) {
	head := string(data[:min(len(data), 4096)])
	var w, h float64
	if idx := strings.Index(head, "<svg"); idx >= 0 {
		sub := head[idx:]
		if end := strings.IndexByte(sub, '>'); end > 0 {
			sub = sub[:end]
		}
		w = parseSVGAttrNum(sub, "width")
		h = parseSVGAttrNum(sub, "height")
		if (w <= 0 || h <= 0) && strings.Contains(sub, "viewBox") {
			if vb := extractAttr(sub, "viewBox"); vb != "" {
				parts := strings.Fields(strings.ReplaceAll(vb, ",", " "))
				if len(parts) == 4 {
					vw, _ := strconv.ParseFloat(parts[2], 64)
					vh, _ := strconv.ParseFloat(parts[3], 64)
					if w <= 0 {
						w = vw
					}
					if h <= 0 {
						h = vh
					}
				}
			}
		}
	}
	return int(w), int(h)
}

func parseSVGAttrNum(tag, attr string) float64 {
	raw := extractAttr(tag, attr)
	if raw == "" || strings.HasSuffix(raw, "%") {
		return 0
	}
	raw = strings.TrimSuffix(strings.TrimSuffix(raw, "px"), "pt")
	v, _ := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	return v
}

func extractAttr(tag, name string) string {
	for _, quote := range []byte{'"', '\''} {
		pat := name + "=" + string(quote)
		if idx := strings.Index(tag, pat); idx >= 0 {
			rest := tag[idx+len(pat):]
			if end := strings.IndexByte(rest, quote); end >= 0 {
				return rest[:end]
			}
		}
	}
	return ""
}

// readBasicEXIF reads Make, Model, DateTime, and Orientation from a JPEG APP1 Exif header.
func readBasicEXIF(r io.Reader, meta *ImageMeta) {
	head := make([]byte, 64*1024)
	n, _ := io.ReadFull(r, head)
	b := head[:n]
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return
	}
	pos := 2
	for pos+4 < len(b) {
		if b[pos] != 0xFF {
			break
		}
		marker := b[pos+1]
		if marker == 0xDA || marker == 0xD9 {
			break
		}
		segLen := int(binary.BigEndian.Uint16(b[pos+2 : pos+4]))
		if segLen < 2 || pos+2+segLen > len(b) {
			break
		}
		if marker == 0xE1 && segLen > 14 && string(b[pos+4:pos+10]) == "Exif\x00\x00" {
			tiff := b[pos+10 : pos+2+segLen]
			parseEXIFTIFF(tiff, meta)
			return
		}
		pos += 2 + segLen
	}
}

func parseEXIFTIFF(tiff []byte, meta *ImageMeta) {
	if len(tiff) < 8 {
		return
	}
	var bo binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return
	}
	if bo.Uint16(tiff[2:4]) != 42 {
		return
	}
	ifd0 := int(bo.Uint32(tiff[4:8]))
	parseIFD(tiff, bo, ifd0, meta)
}

func parseIFD(tiff []byte, bo binary.ByteOrder, off int, meta *ImageMeta) {
	if off < 0 || off+2 > len(tiff) {
		return
	}
	cnt := int(bo.Uint16(tiff[off : off+2]))
	for i := 0; i < cnt; i++ {
		e := off + 2 + i*12
		if e+12 > len(tiff) {
			break
		}
		tag := bo.Uint16(tiff[e : e+2])
		typ := bo.Uint16(tiff[e+2 : e+4])
		num := int(bo.Uint32(tiff[e+4 : e+8]))
		valOff := int(bo.Uint32(tiff[e+8 : e+12]))

		readASCII := func() string {
			if typ != 2 || num <= 0 {
				return ""
			}
			var raw []byte
			if num <= 4 {
				raw = tiff[e+8 : e+8+num]
			} else if valOff >= 0 && valOff+num <= len(tiff) {
				raw = tiff[valOff : valOff+num]
			}
			return strings.TrimRight(string(raw), "\x00 ")
		}

		switch tag {
		case 0x010F:
			meta.CameraMake = readASCII()
		case 0x0110:
			meta.CameraModel = readASCII()
		case 0x0112:
			if typ == 3 {
				meta.Orientation = int(bo.Uint16(tiff[e+8 : e+10]))
			}
		case 0x0132, 0x9003:
			if meta.DateTime == "" {
				meta.DateTime = readASCII()
			}
		case 0x8769:
			parseIFD(tiff, bo, valOff, meta)
		}
	}
}
