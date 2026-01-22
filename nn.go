package downscale

import (
	"context"
	"errors"
	"image"
	"runtime"
	"sync"
)

func NRGBAFast(ctx context.Context, dest *image.NRGBA, src *image.NRGBA) error {
	return nn(
		ctx,
		dest.Pix,
		src.Pix,
		dest.Rect.Dx(),
		dest.Rect.Dy(),
		src.Rect.Dx(),
		src.Rect.Dy(),
	)
}

func RGBAFast(ctx context.Context, dest *image.RGBA, src *image.RGBA) error {
	return nn(
		ctx,
		dest.Pix,
		src.Pix,
		dest.Rect.Dx(),
		dest.Rect.Dy(),
		src.Rect.Dx(),
		src.Rect.Dy(),
	)
}

// NRGBAFastPartial performs partial nearest-neighbor downscaling of NRGBA images.
// Only the destination tiles corresponding to the dirty source tiles are updated.
// srcDirtyTiles are tile coordinates in source image space (in units of srcTileSize).
func NRGBAFastPartial(ctx context.Context, dest *image.NRGBA, src *image.NRGBA, srcTileSize, dstTileSize int, srcDirtyTiles []image.Point) error {
	sw, sh := src.Rect.Dx(), src.Rect.Dy()
	dw, dh := dest.Rect.Dx(), dest.Rect.Dy()
	if dw <= 0 || dh <= 0 {
		return nil
	}
	if sw < dw || sh < dh {
		return errors.New("upscale is not supported")
	}
	if len(srcDirtyTiles) == 0 {
		return nil
	}

	dstDirtyTiles := calcDstDirtyTiles(sw, sh, dw, dh, srcTileSize, dstTileSize, srcDirtyTiles)
	if len(dstDirtyTiles) == 0 {
		return nil
	}

	var h handle
	h.wg.Add(1)
	go func() {
		defer h.Done()
		nnTiledPartial(&h, dest.Pix, src.Pix, dw, dh, sw, sh, dstTileSize, dstDirtyTiles)
	}()
	return h.Wait(ctx)
}

// RGBAFastPartial performs partial nearest-neighbor downscaling of RGBA images.
// Only the destination tiles corresponding to the dirty source tiles are updated.
// srcDirtyTiles are tile coordinates in source image space (in units of srcTileSize).
func RGBAFastPartial(ctx context.Context, dest *image.RGBA, src *image.RGBA, srcTileSize, dstTileSize int, srcDirtyTiles []image.Point) error {
	sw, sh := src.Rect.Dx(), src.Rect.Dy()
	dw, dh := dest.Rect.Dx(), dest.Rect.Dy()
	if dw <= 0 || dh <= 0 {
		return nil
	}
	if sw < dw || sh < dh {
		return errors.New("upscale is not supported")
	}
	if len(srcDirtyTiles) == 0 {
		return nil
	}

	dstDirtyTiles := calcDstDirtyTiles(sw, sh, dw, dh, srcTileSize, dstTileSize, srcDirtyTiles)
	if len(dstDirtyTiles) == 0 {
		return nil
	}

	var h handle
	h.wg.Add(1)
	go func() {
		defer h.Done()
		nnTiledPartial(&h, dest.Pix, src.Pix, dw, dh, sw, sh, dstTileSize, dstDirtyTiles)
	}()
	return h.Wait(ctx)
}

func nnTiledPartial(parentHandle *handle, dPix []byte, sPix []byte, dw, dh, sw, sh, ts int, dstDirtyTiles [][2]uint32) {
	totalTiles := len(dstDirtyTiles)
	n := runtime.GOMAXPROCS(0)
	if n > totalTiles {
		n = totalTiles
	}

	var wg sync.WaitGroup
	tileChan := make(chan [2]uint32, totalTiles)
	for _, tile := range dstDirtyTiles {
		tileChan <- tile
	}
	close(tileChan)

	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			nnProcessTiles(parentHandle, tileChan, dPix, sPix, dw, dh, sw, sh, ts)
		}()
	}
	wg.Wait()
}

func nnProcessTiles(h *handle, tileChan <-chan [2]uint32, dPix []byte, sPix []byte, dw, dh, sw, sh, ts int) {
	dwx4, swx4 := dw<<2, sw<<2
	xStep := (sw << shift) / dw
	xHalf := xStep >> 1
	yStep := (sh << shift) / dh
	yHalf := yStep >> 1

	for tile := range tileChan {
		if h.Aborted() {
			return
		}

		dxStart, dyStart := int(tile[0]), int(tile[1])
		dxEnd := dxStart + ts
		dyEnd := dyStart + ts
		if dxEnd > dw {
			dxEnd = dw
		}
		if dyEnd > dh {
			dyEnd = dh
		}

		yFP := dyStart*yStep + yHalf
		for dy := dyStart; dy < dyEnd; dy++ {
			sy := yFP >> shift
			s := sPix[sy*swx4:]
			d := dPix[dy*dwx4:]

			xFP := dxStart*xStep + xHalf
			for dx := dxStart; dx < dxEnd; dx++ {
				sx := xFP >> shift
				si := sx << 2
				di := dx << 2
				d[di+0] = s[si+0]
				d[di+1] = s[si+1]
				d[di+2] = s[si+2]
				d[di+3] = s[si+3]
				xFP += xStep
			}
			yFP += yStep
		}
	}
}

func nn(ctx context.Context, dPix []byte, sPix []byte, dw int, dh int, sw int, sh int) error {
	if dw <= 0 || dh <= 0 {
		return nil // Nothing to do for zero-size destination
	}

	n := runtime.GOMAXPROCS(0)
	for n > 1 && n<<1 > dh {
		n--
	}

	h := &handle{}
	h.wg.Add(n)
	step := dh / n
	y := 0
	for i := 1; i < n; i++ {
		go nnInner(h, y, y+step, dPix, sPix, dw, dh, sw, sh)
		y += step
	}
	go nnInner(h, y, dh, dPix, sPix, dw, dh, sw, sh)
	return h.Wait(ctx)
}

const shift = 16

func nnInner(h *handle, yMin int, yMax int, dPix []byte, sPix []byte, dw int, dh int, sw int, sh int) {
	defer h.Done()
	dwx4, swx4 := dw<<2, sw<<2

	xStep := (sw << shift) / dw
	xHalf := xStep >> 1
	yStep := (sh << shift) / dh
	yHalf := yStep >> 1

	yFP := yMin*yStep + yHalf
	for dy := yMin; dy < yMax; dy++ {
		if dy&7 == 7 && h.Aborted() {
			return
		}
		sy := yFP >> shift
		s := sPix[sy*swx4:]
		d := dPix[dy*dwx4:]

		xFP := xHalf
		for dx := 0; dx < dw; dx++ {
			sx := xFP >> shift
			si := sx << 2
			di := dx << 2
			d[di+0] = s[si+0]
			d[di+1] = s[si+1]
			d[di+2] = s[si+2]
			d[di+3] = s[si+3]
			xFP += xStep
		}
		yFP += yStep
	}
}
