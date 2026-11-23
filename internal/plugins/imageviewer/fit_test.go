package imageviewer

import (
	"math"
	"testing"

	"github.com/nsgo-dev/nsgo/core"
)

func TestTallImageFitBounds(t *testing.T) {
	view := core.Size{Width: 480, Height: 1100}

	bounds, ok := tallImageFitBounds(view, 1300, 7015)
	if !ok {
		t.Fatal("tall image should start at fit-width")
	}
	if bounds.Size.Width != 1300 {
		t.Fatalf("bounds width = %v, want 1300", bounds.Size.Width)
	}
	wantHeight := 1100 / (480.0 / 1300.0)
	if math.Abs(float64(bounds.Size.Height)-wantHeight) > 0.01 {
		t.Fatalf("bounds height = %v, want %v", bounds.Size.Height, wantHeight)
	}
	if bounds.Size.Height >= 7015 {
		t.Fatalf("visible slice should be smaller than the image: %v", bounds.Size.Height)
	}

	if _, ok := tallImageFitBounds(view, 1300, 700); ok {
		t.Fatal("normal aspect ratio should keep whole-image fit")
	}
	if _, ok := tallImageFitBounds(view, 100, 200); ok {
		t.Fatal("small image should keep whole-image fit")
	}
	if _, ok := tallImageFitBounds(core.Size{}, 1300, 7015); ok {
		t.Fatal("empty view should not fit")
	}
}
