package devviewers

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func testPNG(t *testing.T, fill color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, fill)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func testLayerPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(1, 1, color.RGBA{0, 0, 255, 255})
	img.Set(2, 1, color.RGBA{0, 0, 255, 255})
	img.Set(1, 2, color.RGBA{0, 0, 255, 255})
	img.Set(2, 2, color.RGBA{0, 0, 255, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func zipWithIcons(t *testing.T, files map[string][]byte) *zip.Reader {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range files {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return zr
}

func TestPickAppIconComposesAdaptiveLayers(t *testing.T) {
	zr := zipWithIcons(t, map[string][]byte{
		"res/mipmap-hdpi-v4/ic_launcher_background.png":    testPNG(t, color.RGBA{255, 0, 0, 255}),
		"res/mipmap-hdpi-v4/ic_launcher_foreground.png":    testLayerPNG(t),
		"res/mipmap-xxxhdpi-v4/ic_launcher_background.png": testPNG(t, color.RGBA{255, 0, 0, 255}),
		"res/mipmap-xxxhdpi-v4/ic_launcher_foreground.png": testLayerPNG(t),
	})
	data := pickAppIcon(zr)
	if len(data) == 0 {
		t.Fatal("no icon picked")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if got := img.Bounds().Dx(); got != 4 {
		t.Fatalf("icon width = %d, want 4", got)
	}
	if r, g, b, a := img.At(0, 0).RGBA(); r != 0xffff || g != 0 || b != 0 || a != 0xffff {
		t.Fatalf("background pixel = %04x %04x %04x %04x, want red", r, g, b, a)
	}
	if r, g, b, a := img.At(1, 1).RGBA(); r != 0 || g != 0 || b != 0xffff || a != 0xffff {
		t.Fatalf("foreground pixel = %04x %04x %04x %04x, want blue", r, g, b, a)
	}
}

func TestPickAppIconPrefersComposed(t *testing.T) {
	zr := zipWithIcons(t, map[string][]byte{
		"res/mipmap-hdpi-v4/ic_launcher_background.png": testPNG(t, color.RGBA{255, 0, 0, 255}),
		"res/mipmap-hdpi-v4/ic_launcher_foreground.png": testLayerPNG(t),
		"res/mipmap-xxxhdpi-v4/ic_launcher.png":         testPNG(t, color.RGBA{0, 255, 0, 255}),
	})
	data := pickAppIcon(zr)
	if len(data) == 0 {
		t.Fatal("no icon picked")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if r, g, b, a := img.At(0, 0).RGBA(); r != 0 || g != 0xffff || b != 0 || a != 0xffff {
		t.Fatalf("composed icon pixel = %04x %04x %04x %04x, want green", r, g, b, a)
	}
}
