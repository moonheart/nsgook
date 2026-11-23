package imageviewer

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeQOIAndTransformations(t *testing.T) {
	// Build a valid 2x1 QOI image: red pixel (255,0,0,255) and blue pixel (0,0,255,255)
	var buf bytes.Buffer
	buf.WriteString("qoif")
	_ = binary.Write(&buf, binary.BigEndian, uint32(2))
	_ = binary.Write(&buf, binary.BigEndian, uint32(1))
	buf.WriteByte(4) // RGBA
	buf.WriteByte(0) // sRGB
	buf.Write([]byte{0xFE, 255, 0, 0})
	buf.Write([]byte{0xFE, 0, 0, 255})
	buf.Write([]byte{0, 0, 0, 0, 0, 0, 0, 1})

	img, err := DecodeQOI(buf.Bytes())
	if err != nil {
		t.Fatalf("DecodeQOI failed: %v", err)
	}
	if img.Bounds().Dx() != 2 || img.Bounds().Dy() != 1 {
		t.Fatalf("unexpected QOI bounds: %v", img.Bounds())
	}
	if c := img.RGBAAt(0, 0); c != (color.RGBA{R: 255, A: 255}) {
		t.Fatalf("pixel (0,0) = %+v, want red", c)
	}
	if c := img.RGBAAt(1, 0); c != (color.RGBA{B: 255, A: 255}) {
		t.Fatalf("pixel (1,0) = %+v, want blue", c)
	}

	rot := Rotate90(img, 1)
	if rot.Bounds().Dx() != 1 || rot.Bounds().Dy() != 2 {
		t.Fatalf("unexpected rotated bounds: %v", rot.Bounds())
	}
	if c := rot.RGBAAt(0, 1); c != (color.RGBA{B: 255, A: 255}) {
		t.Fatalf("rotated pixel (0,1) = %+v, want blue", c)
	}

	fl := Flip(img, true, false)
	if c := fl.RGBAAt(0, 0); c != (color.RGBA{B: 255, A: 255}) {
		t.Fatalf("flipped pixel (0,0) = %+v, want blue", c)
	}
}

func TestDecodeICOAndTGAAndNetpbm(t *testing.T) {
	// ICO wrapping a 4x4 PNG
	src := image.NewRGBA(image.Rect(0, 0, 4, 4))
	src.SetRGBA(1, 1, color.RGBA{G: 200, A: 255})
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, src); err != nil {
		t.Fatal(err)
	}
	var ico bytes.Buffer
	_ = binary.Write(&ico, binary.LittleEndian, uint16(0))
	_ = binary.Write(&ico, binary.LittleEndian, uint16(1)) // ICON
	_ = binary.Write(&ico, binary.LittleEndian, uint16(1))
	ico.Write([]byte{4, 4, 0, 0})
	_ = binary.Write(&ico, binary.LittleEndian, uint16(1))
	_ = binary.Write(&ico, binary.LittleEndian, uint16(32))
	_ = binary.Write(&ico, binary.LittleEndian, uint32(pngBuf.Len()))
	_ = binary.Write(&ico, binary.LittleEndian, uint32(22))
	ico.Write(pngBuf.Bytes())

	decIco, err := DecodeICO(ico.Bytes())
	if err != nil {
		t.Fatalf("DecodeICO failed: %v", err)
	}
	if decIco.RGBAAt(1, 1).G != 200 {
		t.Fatalf("unexpected ICO pixel: %+v", decIco.RGBAAt(1, 1))
	}

	// Netpbm P6 2x1
	ppm := []byte("P6\n2 1\n255\n\x10\x20\x30\x40\x50\x60")
	decPPM, err := DecodeNetpbm(ppm)
	if err != nil {
		t.Fatalf("DecodeNetpbm failed: %v", err)
	}
	if c := decPPM.RGBAAt(1, 0); c != (color.RGBA{R: 0x40, G: 0x50, B: 0x60, A: 255}) {
		t.Fatalf("unexpected PPM pixel: %+v", c)
	}

	// Uncompressed top-down 24-bit TGA 2x1
	var tga [18 + 6]byte
	tga[2] = 2
	binary.LittleEndian.PutUint16(tga[12:14], 2)
	binary.LittleEndian.PutUint16(tga[14:16], 1)
	tga[16] = 24
	tga[17] = 0x20 // top-down
	tga[18], tga[19], tga[20] = 9, 8, 7
	decTGA, err := DecodeTGA(tga[:])
	if err != nil {
		t.Fatalf("DecodeTGA failed: %v", err)
	}
	if c := decTGA.RGBAAt(0, 0); c != (color.RGBA{R: 7, G: 8, B: 9, A: 255}) {
		t.Fatalf("unexpected TGA pixel: %+v", c)
	}
}

func TestCanNativeURIAndFileURI(t *testing.T) {
	cases := []struct {
		name string
		meta ImageMeta
		want bool
	}{
		{"png normal", ImageMeta{Width: 3508, Height: 4961, Format: "PNG"}, true},
		{"jpeg unrotated", ImageMeta{Width: 4000, Height: 3000, Format: "JPEG", Orientation: 1}, true},
		{"jpeg exif rotated", ImageMeta{Width: 4000, Height: 3000, Format: "JPEG", Orientation: 6}, false},
		{"webp normal", ImageMeta{Width: 1920, Height: 1080, Format: "WEBP"}, true},
		{"gif still", ImageMeta{Width: 800, Height: 600, Format: "GIF"}, true},
		{"gif animated fits", ImageMeta{Width: 800, Height: 600, Format: "GIF", Animated: true}, true},
		{"gif animated too huge", ImageMeta{Width: 5000, Height: 4000, Format: "GIF", Animated: true}, false},
		{"too large >67MP", ImageMeta{Width: 10000, Height: 8000, Format: "PNG"}, false},
		{"bmp unsupported by native", ImageMeta{Width: 800, Height: 600, Format: "BMP"}, false},
		{"ico unsupported", ImageMeta{Width: 256, Height: 256, Format: "ICO"}, false},
		{"qoi unsupported", ImageMeta{Width: 800, Height: 600, Format: "QOI"}, false},
		{"svg unsupported", ImageMeta{Width: 800, Height: 600, Format: "SVG"}, false},
		{"zero dims", ImageMeta{Width: 0, Height: 600, Format: "PNG"}, false},
	}
	for _, tc := range cases {
		if got := CanNativeURI(tc.meta); got != tc.want {
			t.Errorf("%s: CanNativeURI(%+v) = %v, want %v", tc.name, tc.meta, got, tc.want)
		}
	}

	// Local path with Chinese characters, space, '#' and '?'
	rawLocal := `D:\Downloads\红豆 南瓜#1?.png`
	uriLocal := FileURI(rawLocal, 1700000000, 12345)
	if !strings.HasPrefix(uriLocal, "file:///") || !strings.Contains(uriLocal, "?v=1700000000_12345") {
		t.Fatalf("unexpected local FileURI: %q", uriLocal)
	}
	parsedLocal, err := url.Parse(uriLocal)
	if err != nil {
		t.Fatalf("url.Parse local failed: %v", err)
	}
	if parsedLocal.Host != "" || parsedLocal.Path != `/D:/Downloads/红豆 南瓜#1?.png` || parsedLocal.RawQuery != "v=1700000000_12345" {
		t.Fatalf("unexpected parsed local URL: host=%q path=%q query=%q", parsedLocal.Host, parsedLocal.Path, parsedLocal.RawQuery)
	}

	// UNC share path
	uriUNC := FileURI(`\\nas01\share\photo 1.jpg`, 42, 99)
	parsedUNC, err := url.Parse(uriUNC)
	if err != nil {
		t.Fatalf("url.Parse UNC failed: %v", err)
	}
	if parsedUNC.Host != "nas01" || parsedUNC.Path != "/share/photo 1.jpg" || parsedUNC.RawQuery != "v=42_99" {
		t.Fatalf("unexpected parsed UNC URL: host=%q path=%q query=%q", parsedUNC.Host, parsedUNC.Path, parsedUNC.RawQuery)
	}
}

func TestIsAnimatedGIFAndToRGBANoCopy(t *testing.T) {
	dir := t.TempDir()
	pal := color.Palette{color.Black, color.White}
	f1 := image.NewPaletted(image.Rect(0, 0, 4, 4), pal)
	f2 := image.NewPaletted(image.Rect(0, 0, 4, 4), pal)

	stillPath := filepath.Join(dir, "still.gif")
	var stillBuf bytes.Buffer
	if err := gif.Encode(&stillBuf, f1, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stillPath, stillBuf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if meta := ReadMeta(stillPath); meta.Animated || meta.Width != 4 || meta.Height != 4 || meta.Format != "GIF" {
		t.Fatalf("still gif meta mismatch: %+v", meta)
	}

	animPath := filepath.Join(dir, "anim.gif")
	var animBuf bytes.Buffer
	if err := gif.EncodeAll(&animBuf, &gif.GIF{
		Image: []*image.Paletted{f1, f2},
		Delay: []int{5, 5},
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(animPath, animBuf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if meta := ReadMeta(animPath); !meta.Animated || meta.Width != 4 || meta.Height != 4 || meta.Format != "GIF" {
		t.Fatalf("animated gif meta mismatch: %+v", meta)
	}

	// Verify ToRGBA avoids copying tight (0,0) *image.RGBA inputs.
	tight := image.NewRGBA(image.Rect(0, 0, 8, 8))
	if got := ToRGBA(tight); got != tight {
		t.Fatalf("ToRGBA allocated copy (%p != %p) for tight RGBA", got, tight)
	}
}
