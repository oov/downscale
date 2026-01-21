package downscale

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"testing"
)

// TestNRGBAGammaTransparentPixelBug tests the bug where transparent pixels
// in gamma-corrected downscaling leave stale data in the intermediate buffer.
// The bug occurs because horzRow16NRGBATile and vertCol16NRGBATile don't write
// zeros when alpha is 0, leaving previous buffer contents.
func TestNRGBAGammaTransparentPixelBug(t *testing.T) {
	ctx := context.Background()

	// First: process an opaque image to fill the gamma pool buffer with data
	src1 := image.NewNRGBA(image.Rect(0, 0, 300, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 300; x++ {
			src1.SetNRGBA(x, y, color.NRGBA{255, 128, 64, 255})
		}
	}
	dest1 := image.NewNRGBA(image.Rect(0, 0, 100, 100))
	if err := NRGBAGamma(ctx, dest1, src1, 2.2); err != nil {
		t.Fatalf("First NRGBAGamma failed: %v", err)
	}

	// Second: process a mostly transparent image
	// If the bug exists, transparent areas will show data from src1
	src2 := image.NewNRGBA(image.Rect(0, 0, 300, 300))
	// Only top-left corner is opaque
	for y := 0; y < 30; y++ {
		for x := 0; x < 30; x++ {
			src2.SetNRGBA(x, y, color.NRGBA{0, 255, 0, 255})
		}
	}

	dest2 := image.NewNRGBA(image.Rect(0, 0, 100, 100))
	if err := NRGBAGamma(ctx, dest2, src2, 2.2); err != nil {
		t.Fatalf("Second NRGBAGamma failed: %v", err)
	}

	// Check transparent region for garbage
	for y := 20; y < 100; y++ {
		for x := 20; x < 100; x++ {
			idx := (y*100 + x) * 4
			r, g, b, a := dest2.Pix[idx], dest2.Pix[idx+1], dest2.Pix[idx+2], dest2.Pix[idx+3]

			// Transparent pixel should have RGB=0 or be fully transparent
			if a == 0 && (r != 0 || g != 0 || b != 0) {
				t.Fatalf("at (%d,%d): transparent pixel has garbage RGB: (%d,%d,%d,%d)",
					x, y, r, g, b, a)
			}
			// Check for orange color leaked from src1
			if r > 200 && g > 100 && g < 180 && b > 40 && b < 100 {
				t.Fatalf("at (%d,%d): data from previous operation leaked: (%d,%d,%d,%d)",
					x, y, r, g, b, a)
			}
		}
	}
}

// TestRGBAGammaTransparentPixelBug tests the same bug for RGBAGamma.
func TestRGBAGammaTransparentPixelBug(t *testing.T) {
	ctx := context.Background()

	src1 := image.NewRGBA(image.Rect(0, 0, 300, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 300; x++ {
			src1.SetRGBA(x, y, color.RGBA{255, 128, 64, 255})
		}
	}
	dest1 := image.NewRGBA(image.Rect(0, 0, 100, 100))
	if err := RGBAGamma(ctx, dest1, src1, 2.2); err != nil {
		t.Fatalf("First RGBAGamma failed: %v", err)
	}

	src2 := image.NewRGBA(image.Rect(0, 0, 300, 300))
	for y := 0; y < 30; y++ {
		for x := 0; x < 30; x++ {
			src2.SetRGBA(x, y, color.RGBA{0, 255, 0, 255})
		}
	}

	dest2 := image.NewRGBA(image.Rect(0, 0, 100, 100))
	if err := RGBAGamma(ctx, dest2, src2, 2.2); err != nil {
		t.Fatalf("Second RGBAGamma failed: %v", err)
	}

	for y := 20; y < 100; y++ {
		for x := 20; x < 100; x++ {
			idx := (y*100 + x) * 4
			r, g, b, a := dest2.Pix[idx], dest2.Pix[idx+1], dest2.Pix[idx+2], dest2.Pix[idx+3]

			if a == 0 && (r != 0 || g != 0 || b != 0) {
				t.Fatalf("at (%d,%d): transparent pixel has garbage RGB: (%d,%d,%d,%d)",
					x, y, r, g, b, a)
			}
			if r > 200 && g > 100 && g < 180 && b > 40 && b < 100 {
				t.Fatalf("at (%d,%d): data from previous operation leaked: (%d,%d,%d,%d)",
					x, y, r, g, b, a)
			}
		}
	}
}

func TestRGBACorrectness(t *testing.T) {
	tests := []struct {
		name string
		sw   int
		sh   int
		dw   int
		dh   int
	}{
		{"small", 100, 100, 50, 50},
		{"medium", 400, 300, 122, 133},
		{"large", 4000, 3000, 1222, 1333},
		{"non_divisible", 1000, 800, 333, 277},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create source image with gradient
			src := image.NewRGBA(image.Rect(0, 0, tt.sw, tt.sh))
			for y := 0; y < tt.sh; y++ {
				for x := 0; x < tt.sw; x++ {
					src.SetRGBA(x, y, color.RGBA{
						R: uint8(x * 255 / tt.sw),
						G: uint8(y * 255 / tt.sh),
						B: uint8((x + y) * 255 / (tt.sw + tt.sh)),
						A: 255,
					})
				}
			}

			ctx := context.Background()

			dest := image.NewRGBA(image.Rect(0, 0, tt.dw, tt.dh))
			if err := RGBA(ctx, dest, src); err != nil {
				t.Fatalf("RGBA failed: %v", err)
			}

			// Verify output is not all zeros
			hasNonZero := false
			for _, b := range dest.Pix {
				if b != 0 {
					hasNonZero = true
					break
				}
			}
			if !hasNonZero {
				t.Error("Output is all zeros")
			}
		})
	}
}

func TestRGBAWithTransparency(t *testing.T) {
	sw, sh := 200, 150
	dw, dh := 100, 75

	src := image.NewRGBA(image.Rect(0, 0, sw, sh))
	// Create checkerboard with transparency
	for y := 0; y < sh; y++ {
		for x := 0; x < sw; x++ {
			if (x+y)%2 == 0 {
				src.SetRGBA(x, y, color.RGBA{255, 0, 0, 255})
			} else {
				src.SetRGBA(x, y, color.RGBA{0, 255, 0, 128})
			}
		}
	}

	ctx := context.Background()

	dest := image.NewRGBA(image.Rect(0, 0, dw, dh))
	if err := RGBA(ctx, dest, src); err != nil {
		t.Fatalf("RGBA failed: %v", err)
	}

	// Verify output is not all zeros
	hasNonZero := false
	for _, b := range dest.Pix {
		if b != 0 {
			hasNonZero = true
			break
		}
	}
	if !hasNonZero {
		t.Error("Output is all zeros with transparency")
	}
}

func TestRGBASameSize(t *testing.T) {
	sw, sh := 100, 100
	src := image.NewRGBA(image.Rect(0, 0, sw, sh))
	draw.Draw(src, src.Rect, image.Opaque, image.Point{}, draw.Src)

	ctx := context.Background()

	dest := image.NewRGBA(image.Rect(0, 0, sw, sh))
	if err := RGBA(ctx, dest, src); err != nil {
		t.Fatalf("RGBA failed: %v", err)
	}

	// Same size should just copy
	for i := range dest.Pix {
		if dest.Pix[i] != src.Pix[i] {
			t.Errorf("Same size should just copy, but differs at %d", i)
			break
		}
	}
}

func TestNRGBACorrectness(t *testing.T) {
	tests := []struct {
		name string
		sw   int
		sh   int
		dw   int
		dh   int
	}{
		{"small", 100, 100, 50, 50},
		{"medium", 400, 300, 122, 133},
		{"large", 4000, 3000, 1222, 1333},
		{"non_divisible", 1000, 800, 333, 277},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := image.NewNRGBA(image.Rect(0, 0, tt.sw, tt.sh))
			for y := 0; y < tt.sh; y++ {
				for x := 0; x < tt.sw; x++ {
					src.SetNRGBA(x, y, color.NRGBA{
						R: uint8(x * 255 / tt.sw),
						G: uint8(y * 255 / tt.sh),
						B: uint8((x + y) * 255 / (tt.sw + tt.sh)),
						A: 255,
					})
				}
			}

			ctx := context.Background()

			dest := image.NewNRGBA(image.Rect(0, 0, tt.dw, tt.dh))
			if err := NRGBA(ctx, dest, src); err != nil {
				t.Fatalf("NRGBA failed: %v", err)
			}

			// Verify output is not all zeros
			hasNonZero := false
			for _, b := range dest.Pix {
				if b != 0 {
					hasNonZero = true
					break
				}
			}
			if !hasNonZero {
				t.Error("Output is all zeros")
			}
		})
	}
}

func TestRGBAGammaCorrectness(t *testing.T) {
	tests := []struct {
		name string
		sw   int
		sh   int
		dw   int
		dh   int
	}{
		{"small", 100, 100, 50, 50},
		{"medium", 400, 300, 122, 133},
		{"large", 4000, 3000, 1222, 1333},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := image.NewRGBA(image.Rect(0, 0, tt.sw, tt.sh))
			for y := 0; y < tt.sh; y++ {
				for x := 0; x < tt.sw; x++ {
					src.SetRGBA(x, y, color.RGBA{
						R: uint8(x * 255 / tt.sw),
						G: uint8(y * 255 / tt.sh),
						B: uint8((x + y) * 255 / (tt.sw + tt.sh)),
						A: 255,
					})
				}
			}

			ctx := context.Background()

			dest := image.NewRGBA(image.Rect(0, 0, tt.dw, tt.dh))
			if err := RGBAGamma(ctx, dest, src, 2.2); err != nil {
				t.Fatalf("RGBAGamma failed: %v", err)
			}

			// Verify output is not all zeros
			hasNonZero := false
			for _, b := range dest.Pix {
				if b != 0 {
					hasNonZero = true
					break
				}
			}
			if !hasNonZero {
				t.Error("Output is all zeros")
			}
		})
	}
}

func TestRGBAFastCorrectness(t *testing.T) {
	tests := []struct {
		name string
		sw   int
		sh   int
		dw   int
		dh   int
	}{
		{"small", 100, 100, 50, 50},
		{"medium", 400, 300, 122, 133},
		{"large", 4000, 3000, 1222, 1333},
		{"non_divisible", 1000, 800, 333, 277},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := image.NewRGBA(image.Rect(0, 0, tt.sw, tt.sh))
			for y := 0; y < tt.sh; y++ {
				for x := 0; x < tt.sw; x++ {
					src.SetRGBA(x, y, color.RGBA{
						R: uint8(x * 255 / tt.sw),
						G: uint8(y * 255 / tt.sh),
						B: uint8((x + y) * 255 / (tt.sw + tt.sh)),
						A: 255,
					})
				}
			}

			ctx := context.Background()

			dest := image.NewRGBA(image.Rect(0, 0, tt.dw, tt.dh))
			if err := RGBAFast(ctx, dest, src); err != nil {
				t.Fatalf("RGBAFast failed: %v", err)
			}

			// Verify output is not all zeros
			hasNonZero := false
			for _, b := range dest.Pix {
				if b != 0 {
					hasNonZero = true
					break
				}
			}
			if !hasNonZero {
				t.Error("Output is all zeros")
			}
		})
	}
}
