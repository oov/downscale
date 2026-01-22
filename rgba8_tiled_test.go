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

// TestNRGBAFastPartialCorrectness tests that NRGBAFastPartial produces the same result
// as a full NRGBAFast for the updated tiles.
func TestNRGBAFastPartialCorrectness(t *testing.T) {
	sw, sh := 256, 256
	dw, dh := 128, 128
	tileSize := 64

	src := image.NewNRGBA(image.Rect(0, 0, sw, sh))
	for y := 0; y < sh; y++ {
		for x := 0; x < sw; x++ {
			src.SetNRGBA(x, y, color.NRGBA{
				R: uint8(x),
				G: uint8(y),
				B: uint8((x + y) / 2),
				A: 255,
			})
		}
	}

	ctx := context.Background()

	fullDest := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	if err := NRGBAFast(ctx, fullDest, src); err != nil {
		t.Fatalf("Full NRGBAFast failed: %v", err)
	}

	partialDest := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	dirtyTiles := []image.Point{}
	for y := 0; y < sh; y += tileSize {
		for x := 0; x < sw; x += tileSize {
			dirtyTiles = append(dirtyTiles, image.Pt(x, y))
		}
	}
	if err := NRGBAFastPartial(ctx, partialDest, src, tileSize, tileSize, dirtyTiles); err != nil {
		t.Fatalf("NRGBAFastPartial failed: %v", err)
	}

	for i := range fullDest.Pix {
		if fullDest.Pix[i] != partialDest.Pix[i] {
			y := i / 4 / dw
			x := (i / 4) % dw
			c := i % 4
			t.Errorf("Mismatch at (%d,%d) channel %d: full=%d partial=%d",
				x, y, c, fullDest.Pix[i], partialDest.Pix[i])
			break
		}
	}
}

// TestNRGBAFastPartialSingleTile tests partial update of a single tile.
func TestNRGBAFastPartialSingleTile(t *testing.T) {
	sw, sh := 256, 256
	dw, dh := 128, 128
	tileSize := 64

	ctx := context.Background()

	src := image.NewNRGBA(image.Rect(0, 0, sw, sh))
	for y := 0; y < sh; y++ {
		for x := 0; x < sw; x++ {
			src.SetNRGBA(x, y, color.NRGBA{255, 0, 0, 255})
		}
	}

	dest := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	if err := NRGBAFast(ctx, dest, src); err != nil {
		t.Fatalf("Initial NRGBAFast failed: %v", err)
	}

	for y := 0; y < tileSize; y++ {
		for x := 0; x < tileSize; x++ {
			src.SetNRGBA(x, y, color.NRGBA{0, 255, 0, 255})
		}
	}

	dirtyTiles := []image.Point{{0, 0}}
	if err := NRGBAFastPartial(ctx, dest, src, tileSize, tileSize, dirtyTiles); err != nil {
		t.Fatalf("NRGBAFastPartial failed: %v", err)
	}

	tlIdx := 0
	if dest.Pix[tlIdx+1] < 200 {
		t.Errorf("Top-left should be green, got G=%d", dest.Pix[tlIdx+1])
	}

	brIdx := ((dh - 1) * dw + (dw - 1)) * 4
	if dest.Pix[brIdx] < 200 {
		t.Errorf("Bottom-right should be red, got R=%d", dest.Pix[brIdx])
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

// TestNRGBAPartialCorrectness tests that NRGBAPartial produces the same result
// as a full NRGBA for the updated tiles.
func TestNRGBAPartialCorrectness(t *testing.T) {
	sw, sh := 256, 256
	dw, dh := 128, 128
	tileSize := 64

	// Create source with a pattern
	src := image.NewNRGBA(image.Rect(0, 0, sw, sh))
	for y := 0; y < sh; y++ {
		for x := 0; x < sw; x++ {
			src.SetNRGBA(x, y, color.NRGBA{
				R: uint8(x),
				G: uint8(y),
				B: uint8((x + y) / 2),
				A: 255,
			})
		}
	}

	ctx := context.Background()

	// Full downscale for reference (using NRGBA, not NRGBAFast)
	fullDest := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	if err := NRGBA(ctx, fullDest, src); err != nil {
		t.Fatalf("Full NRGBA failed: %v", err)
	}

	// Partial downscale with all tiles marked dirty (should produce same result)
	partialDest := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	dirtyTiles := []image.Point{}
	for y := 0; y < sh; y += tileSize {
		for x := 0; x < sw; x += tileSize {
			dirtyTiles = append(dirtyTiles, image.Pt(x, y))
		}
	}
	if err := NRGBAPartial(ctx, partialDest, src, tileSize, tileSize, dirtyTiles); err != nil {
		t.Fatalf("NRGBAPartial failed: %v", err)
	}

	// Compare results
	for i := range fullDest.Pix {
		if fullDest.Pix[i] != partialDest.Pix[i] {
			y := i / 4 / dw
			x := (i / 4) % dw
			c := i % 4
			t.Errorf("Mismatch at (%d,%d) channel %d: full=%d partial=%d",
				x, y, c, fullDest.Pix[i], partialDest.Pix[i])
			break
		}
	}
}

// TestNRGBAPartialSingleTile tests partial update of a single tile.
func TestNRGBAPartialSingleTile(t *testing.T) {
	sw, sh := 256, 256
	dw, dh := 128, 128
	tileSize := 64

	ctx := context.Background()

	// Create initial source (all red)
	src := image.NewNRGBA(image.Rect(0, 0, sw, sh))
	for y := 0; y < sh; y++ {
		for x := 0; x < sw; x++ {
			src.SetNRGBA(x, y, color.NRGBA{255, 0, 0, 255})
		}
	}

	// Initial full downscale (using NRGBA, not NRGBAFast)
	dest := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	if err := NRGBA(ctx, dest, src); err != nil {
		t.Fatalf("Initial NRGBA failed: %v", err)
	}

	// Modify one tile in source (top-left) to green
	for y := 0; y < tileSize; y++ {
		for x := 0; x < tileSize; x++ {
			src.SetNRGBA(x, y, color.NRGBA{0, 255, 0, 255})
		}
	}

	// Partial update for modified tile only
	dirtyTiles := []image.Point{{0, 0}}
	if err := NRGBAPartial(ctx, dest, src, tileSize, tileSize, dirtyTiles); err != nil {
		t.Fatalf("NRGBAPartial failed: %v", err)
	}

	// Verify: top-left should be green, rest should be red
	// Check top-left (should be mostly green)
	tlIdx := 0
	if dest.Pix[tlIdx+1] < 200 { // G channel
		t.Errorf("Top-left should be green, got G=%d", dest.Pix[tlIdx+1])
	}

	// Check bottom-right (should still be red)
	brIdx := ((dh - 1) * dw + (dw - 1)) * 4
	if dest.Pix[brIdx] < 200 { // R channel
		t.Errorf("Bottom-right should be red, got R=%d", dest.Pix[brIdx])
	}
}

// TestNRGBAGammaPartialCorrectness tests that NRGBAGammaPartial produces similar
// results to full NRGBAGamma for the updated tiles.
func TestNRGBAGammaPartialCorrectness(t *testing.T) {
	sw, sh := 256, 256
	dw, dh := 128, 128
	tileSize := 64

	// Create source with a pattern
	src := image.NewNRGBA(image.Rect(0, 0, sw, sh))
	for y := 0; y < sh; y++ {
		for x := 0; x < sw; x++ {
			src.SetNRGBA(x, y, color.NRGBA{
				R: uint8(x),
				G: uint8(y),
				B: uint8((x + y) / 2),
				A: 255,
			})
		}
	}

	ctx := context.Background()

	// Full downscale for reference
	fullDest := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	if err := NRGBAGamma(ctx, fullDest, src, 2.2); err != nil {
		t.Fatalf("Full NRGBAGamma failed: %v", err)
	}

	// Partial downscale with all tiles marked dirty
	partialDest := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	dirtyTiles := []image.Point{}
	for y := 0; y < sh; y += tileSize {
		for x := 0; x < sw; x += tileSize {
			dirtyTiles = append(dirtyTiles, image.Pt(x, y))
		}
	}
	if err := NRGBAGammaPartial(ctx, partialDest, src, 2.2, tileSize, tileSize, dirtyTiles); err != nil {
		t.Fatalf("NRGBAGammaPartial failed: %v", err)
	}

	// Compare results (allow small differences due to floating point)
	maxDiff := 0
	for i := range fullDest.Pix {
		diff := int(fullDest.Pix[i]) - int(partialDest.Pix[i])
		if diff < 0 {
			diff = -diff
		}
		if diff > maxDiff {
			maxDiff = diff
		}
		if diff > 2 { // Allow up to 2 levels difference
			y := i / 4 / dw
			x := (i / 4) % dw
			c := i % 4
			t.Errorf("Large mismatch at (%d,%d) channel %d: full=%d partial=%d",
				x, y, c, fullDest.Pix[i], partialDest.Pix[i])
			break
		}
	}
	t.Logf("Max pixel difference: %d", maxDiff)
}

// TestNRGBAGammaPartialSingleTile tests gamma partial update of a single tile.
func TestNRGBAGammaPartialSingleTile(t *testing.T) {
	sw, sh := 256, 256
	dw, dh := 128, 128
	tileSize := 64

	ctx := context.Background()

	// Create initial source (all red)
	src := image.NewNRGBA(image.Rect(0, 0, sw, sh))
	for y := 0; y < sh; y++ {
		for x := 0; x < sw; x++ {
			src.SetNRGBA(x, y, color.NRGBA{255, 0, 0, 255})
		}
	}

	// Initial full downscale
	dest := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	if err := NRGBAGamma(ctx, dest, src, 2.2); err != nil {
		t.Fatalf("Initial NRGBAGamma failed: %v", err)
	}

	// Modify one tile in source (top-left) to green
	for y := 0; y < tileSize; y++ {
		for x := 0; x < tileSize; x++ {
			src.SetNRGBA(x, y, color.NRGBA{0, 255, 0, 255})
		}
	}

	// Partial update for modified tile only
	dirtyTiles := []image.Point{{0, 0}}
	if err := NRGBAGammaPartial(ctx, dest, src, 2.2, tileSize, tileSize, dirtyTiles); err != nil {
		t.Fatalf("NRGBAGammaPartial failed: %v", err)
	}

	// Verify: top-left should be green, rest should be red
	// Check top-left (should be mostly green)
	tlIdx := 0
	if dest.Pix[tlIdx+1] < 200 { // G channel
		t.Errorf("Top-left should be green, got G=%d", dest.Pix[tlIdx+1])
	}

	// Check bottom-right (should still be red)
	brIdx := ((dh - 1) * dw + (dw - 1)) * 4
	if dest.Pix[brIdx] < 200 { // R channel
		t.Errorf("Bottom-right should be red, got R=%d", dest.Pix[brIdx])
	}
}

// TestRGBAPartialCorrectness tests that RGBAPartial produces the same result as RGBA for dirty tiles.
func TestRGBAPartialCorrectness(t *testing.T) {
	sw, sh := 128, 128
	dw, dh := 64, 64
	srcTileSize := 32
	dstTileSize := 16

	ctx := context.Background()

	// Create premultiplied RGBA source with varied colors
	src := image.NewRGBA(image.Rect(0, 0, sw, sh))
	for y := 0; y < sh; y++ {
		for x := 0; x < sw; x++ {
			r := uint8((x * 255) / sw)
			g := uint8((y * 255) / sh)
			b := uint8(128)
			a := uint8(200)
			// Premultiply
			src.SetRGBA(x, y, color.RGBA{
				R: uint8(uint32(r) * uint32(a) / 255),
				G: uint8(uint32(g) * uint32(a) / 255),
				B: uint8(uint32(b) * uint32(a) / 255),
				A: a,
			})
		}
	}

	// Full downscale
	fullDest := image.NewRGBA(image.Rect(0, 0, dw, dh))
	if err := RGBA(ctx, fullDest, src); err != nil {
		t.Fatalf("RGBA failed: %v", err)
	}

	// Partial downscale with all tiles marked dirty
	partialDest := image.NewRGBA(image.Rect(0, 0, dw, dh))
	srcTilesX := (sw + srcTileSize - 1) / srcTileSize
	srcTilesY := (sh + srcTileSize - 1) / srcTileSize
	allDirty := make([]image.Point, 0, srcTilesX*srcTilesY)
	for ty := 0; ty < srcTilesY; ty++ {
		for tx := 0; tx < srcTilesX; tx++ {
			allDirty = append(allDirty, image.Point{X: tx * srcTileSize, Y: ty * srcTileSize})
		}
	}
	if err := RGBAPartial(ctx, partialDest, src, srcTileSize, dstTileSize, allDirty); err != nil {
		t.Fatalf("RGBAPartial failed: %v", err)
	}

	// Compare results
	for i := range fullDest.Pix {
		if fullDest.Pix[i] != partialDest.Pix[i] {
			y := i / 4 / dw
			x := (i / 4) % dw
			c := i % 4
			t.Errorf("Mismatch at (%d,%d) channel %d: full=%d partial=%d",
				x, y, c, fullDest.Pix[i], partialDest.Pix[i])
		}
	}
}

// TestRGBAFastPartialCorrectness tests that RGBAFastPartial produces the same result as RGBAFast for dirty tiles.
func TestRGBAFastPartialCorrectness(t *testing.T) {
	sw, sh := 128, 128
	dw, dh := 64, 64
	srcTileSize := 32
	dstTileSize := 16

	ctx := context.Background()

	// Create premultiplied RGBA source
	src := image.NewRGBA(image.Rect(0, 0, sw, sh))
	for y := 0; y < sh; y++ {
		for x := 0; x < sw; x++ {
			r := uint8((x * 255) / sw)
			g := uint8((y * 255) / sh)
			b := uint8(128)
			a := uint8(200)
			src.SetRGBA(x, y, color.RGBA{
				R: uint8(uint32(r) * uint32(a) / 255),
				G: uint8(uint32(g) * uint32(a) / 255),
				B: uint8(uint32(b) * uint32(a) / 255),
				A: a,
			})
		}
	}

	// Full downscale
	fullDest := image.NewRGBA(image.Rect(0, 0, dw, dh))
	if err := RGBAFast(ctx, fullDest, src); err != nil {
		t.Fatalf("RGBAFast failed: %v", err)
	}

	// Partial downscale with all tiles marked dirty
	partialDest := image.NewRGBA(image.Rect(0, 0, dw, dh))
	srcTilesX := (sw + srcTileSize - 1) / srcTileSize
	srcTilesY := (sh + srcTileSize - 1) / srcTileSize
	allDirty := make([]image.Point, 0, srcTilesX*srcTilesY)
	for ty := 0; ty < srcTilesY; ty++ {
		for tx := 0; tx < srcTilesX; tx++ {
			allDirty = append(allDirty, image.Point{X: tx * srcTileSize, Y: ty * srcTileSize})
		}
	}
	if err := RGBAFastPartial(ctx, partialDest, src, srcTileSize, dstTileSize, allDirty); err != nil {
		t.Fatalf("RGBAFastPartial failed: %v", err)
	}

	// Compare results
	for i := range fullDest.Pix {
		if fullDest.Pix[i] != partialDest.Pix[i] {
			y := i / 4 / dw
			x := (i / 4) % dw
			c := i % 4
			t.Errorf("Mismatch at (%d,%d) channel %d: full=%d partial=%d",
				x, y, c, fullDest.Pix[i], partialDest.Pix[i])
		}
	}
}

// TestRGBAGammaPartialCorrectness tests that RGBAGammaPartial produces the same result as RGBAGamma for dirty tiles.
func TestRGBAGammaPartialCorrectness(t *testing.T) {
	sw, sh := 128, 128
	dw, dh := 64, 64
	srcTileSize := 32
	dstTileSize := 16

	ctx := context.Background()

	// Create premultiplied RGBA source
	src := image.NewRGBA(image.Rect(0, 0, sw, sh))
	for y := 0; y < sh; y++ {
		for x := 0; x < sw; x++ {
			r := uint8((x * 255) / sw)
			g := uint8((y * 255) / sh)
			b := uint8(128)
			a := uint8(200)
			src.SetRGBA(x, y, color.RGBA{
				R: uint8(uint32(r) * uint32(a) / 255),
				G: uint8(uint32(g) * uint32(a) / 255),
				B: uint8(uint32(b) * uint32(a) / 255),
				A: a,
			})
		}
	}

	// Full downscale
	fullDest := image.NewRGBA(image.Rect(0, 0, dw, dh))
	if err := RGBAGamma(ctx, fullDest, src, 2.2); err != nil {
		t.Fatalf("RGBAGamma failed: %v", err)
	}

	// Partial downscale with all tiles marked dirty
	partialDest := image.NewRGBA(image.Rect(0, 0, dw, dh))
	srcTilesX := (sw + srcTileSize - 1) / srcTileSize
	srcTilesY := (sh + srcTileSize - 1) / srcTileSize
	allDirty := make([]image.Point, 0, srcTilesX*srcTilesY)
	for ty := 0; ty < srcTilesY; ty++ {
		for tx := 0; tx < srcTilesX; tx++ {
			allDirty = append(allDirty, image.Point{X: tx * srcTileSize, Y: ty * srcTileSize})
		}
	}
	if err := RGBAGammaPartial(ctx, partialDest, src, 2.2, srcTileSize, dstTileSize, allDirty); err != nil {
		t.Fatalf("RGBAGammaPartial failed: %v", err)
	}

	// Compare results - allow small differences due to floating-point rounding
	maxDiff := 0
	for i := range fullDest.Pix {
		diff := int(fullDest.Pix[i]) - int(partialDest.Pix[i])
		if diff < 0 {
			diff = -diff
		}
		if diff > maxDiff {
			maxDiff = diff
		}
		if diff > 2 { // Allow up to 2 levels difference
			y := i / 4 / dw
			x := (i / 4) % dw
			c := i % 4
			t.Errorf("Large mismatch at (%d,%d) channel %d: full=%d partial=%d",
				x, y, c, fullDest.Pix[i], partialDest.Pix[i])
			break
		}
	}
	t.Logf("Max pixel difference: %d", maxDiff)
}
