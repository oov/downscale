//go:generate go run gentable.go

package downscale

import (
	"context"
	"errors"
	"image"
	"runtime"
	"sync"
)

const (
	// tileSize is the size of the tile for cache-friendly processing.
	// 64x64 RGBA = 16KB fits in L1 cache.
	tileSize = 64
)

var rgbaTilePool = sync.Pool{
	New: func() any {
		// Buffer for intermediate tile data: needs to hold source tile height * dest tile width
		// Maximum: (tileSize * scale_ratio) * tileSize * 4 bytes
		// For reasonable scale ratios (up to 4x), allocate 4 * tileSize^2 * 4 bytes
		return make([]byte, tileSize*tileSize*4*4)
	},
}

// RGBA performs cache-friendly tiled downscaling of RGBA images.
func RGBA(ctx context.Context, dest *image.RGBA, src *image.RGBA) error {
	sw, sh := src.Rect.Dx(), src.Rect.Dy()
	dw, dh := dest.Rect.Dx(), dest.Rect.Dy()
	if dw <= 0 || dh <= 0 {
		return nil // Nothing to do for zero-size destination
	}
	if sw < dw || sh < dh {
		return errors.New("upscale is not supported")
	}
	if sw == dw && sh == dh {
		copy(dest.Pix, src.Pix)
		return nil
	}

	var h handle
	h.wg.Add(1)
	go func() {
		defer h.Done()
		tiledRGBA(&h, dest, src)
	}()
	return h.Wait(ctx)
}

// tiledRGBA processes the image in tiles for better cache locality.
func tiledRGBA(parentHandle *handle, dest *image.RGBA, src *image.RGBA) {
	sw, sh := uint32(src.Rect.Dx()), uint32(src.Rect.Dy())
	dw, dh := uint32(dest.Rect.Dx()), uint32(dest.Rect.Dy())

	// Calculate LCM-based parameters for horizontal scaling
	hLcmLen := lcm(sw, dw)
	hSLcmLen, hDLcmLen := hLcmLen/sw, hLcmLen/dw
	hTT, hFT := makeTable(dw, hDLcmLen, hSLcmLen)

	// Calculate LCM-based parameters for vertical scaling
	vLcmLen := lcm(sh, dh)
	vSLcmLen, vDLcmLen := vLcmLen/sh, vLcmLen/dh
	vTT, vFT := makeTable(dh, vDLcmLen, vSLcmLen)

	// Determine number of workers based on number of tiles
	numTilesX := int((dw + tileSize - 1) / tileSize)
	numTilesY := int((dh + tileSize - 1) / tileSize)
	totalTiles := numTilesX * numTilesY

	n := runtime.GOMAXPROCS(0)
	if n > totalTiles {
		n = totalTiles
	}

	var wg sync.WaitGroup
	tileChan := make(chan [2]uint32, totalTiles)

	// Enqueue all tiles
	for ty := uint32(0); ty < dh; ty += tileSize {
		for tx := uint32(0); tx < dw; tx += tileSize {
			tileChan <- [2]uint32{tx, ty}
		}
	}
	close(tileChan)

	// Start workers
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			processTilesRGBA(parentHandle, tileChan, dest, src, hTT, hFT, vTT, vFT,
				hSLcmLen, hDLcmLen, vSLcmLen, vDLcmLen, sw, dw, sh, dh)
		}()
	}

	// Wait for workers - parent's h.Wait(ctx) handles context cancellation
	// and will call SetAbort(), which workers check via parentHandle.Aborted()
	wg.Wait()
}

func processTilesRGBA(h *handle, tileChan <-chan [2]uint32,
	dest *image.RGBA, src *image.RGBA,
	hTT, hFT, vTT, vFT []uint32,
	hSLcmLen, hDLcmLen, vSLcmLen, vDLcmLen uint32,
	sw, dw, sh, dh uint32) {

	// Get a buffer from the pool for intermediate horizontal results
	buf := rgbaTilePool.Get().([]byte)
	defer rgbaTilePool.Put(buf)

	swx4 := sw << 2
	dwx4 := dw << 2

	for tile := range tileChan {
		if h.Aborted() {
			return
		}

		dxStart, dyStart := tile[0], tile[1]
		dxEnd := dxStart + tileSize
		dyEnd := dyStart + tileSize
		if dxEnd > dw {
			dxEnd = dw
		}
		if dyEnd > dh {
			dyEnd = dh
		}
		tileW := dxEnd - dxStart

		// Calculate corresponding source Y range for this tile
		syStart := vTT[dyStart]
		syEnd := vTT[dyEnd]
		if dyEnd < dh && vFT[dyEnd-1] > 0 {
			syEnd++
		}
		if syEnd > sh {
			syEnd = sh
		}
		srcTileH := syEnd - syStart

		// Calculate intermediate buffer size: srcTileH rows * tileW pixels * 4 bytes
		intermediateStride := tileW << 2
		intermediateSize := srcTileH * intermediateStride
		var intermediate []byte
		if int(intermediateSize) <= len(buf) {
			intermediate = buf[:intermediateSize]
		} else {
			intermediate = make([]byte, intermediateSize)
		}

		// Step 1: Horizontal scaling for source rows [syStart, syEnd)
		// Output to intermediate buffer with width = tileW
		for sy := syStart; sy < syEnd; sy++ {
			srcRow := src.Pix[sy*swx4:]
			dstRow := intermediate[(sy-syStart)*intermediateStride:]
			horzRowRGBATile(dstRow, srcRow, dxStart, dxEnd, hTT, hFT, hSLcmLen, hDLcmLen)
		}

		// Step 2: Vertical scaling from intermediate to destination
		// Process each column in the tile
		for dx := dxStart; dx < dxEnd; dx++ {
			vertColRGBATile(dest.Pix, intermediate,
				dx, dyStart, dyEnd,
				dx-dxStart, syStart,
				vTT, vFT, vSLcmLen, vDLcmLen,
				dwx4, intermediateStride)
		}
	}
}

// horzRowRGBATile performs horizontal scaling for a portion of a row [dxStart, dxEnd).
func horzRowRGBATile(d []byte, s []byte, dxStart, dxEnd uint32, tt, ft []uint32, slcmlen, dlcmlen uint32) {
	di := uint32(0)

	// Initialize fr correctly for starting at dxStart
	fr := uint32(0)
	if dxStart > 0 {
		fr = ft[dxStart-1]
	}

	for dx := dxStart; dx < dxEnd; dx++ {
		tl, tr := tt[dx], tt[dx+1]
		fl := slcmlen - fr
		fr = ft[dx]

		var ta, a, r, g, b, w uint32
		si := tl << 2

		if fl != 0 {
			ta = uint32(s[si+3])
			if ta > 0 {
				w = ta * fl
				r += uint32(divTable[(uint32(s[si+0])<<8)+ta]) * w
				g += uint32(divTable[(uint32(s[si+1])<<8)+ta]) * w
				b += uint32(divTable[(uint32(s[si+2])<<8)+ta]) * w
				a += w
			}
			si += 4
		}
		for i := tl + 1; i < tr; i++ {
			ta = uint32(s[si+3])
			if ta > 0 {
				w = ta * slcmlen
				r += uint32(divTable[(uint32(s[si+0])<<8)+ta]) * w
				g += uint32(divTable[(uint32(s[si+1])<<8)+ta]) * w
				b += uint32(divTable[(uint32(s[si+2])<<8)+ta]) * w
				a += w
			}
			si += 4
		}
		if fr != 0 && s[si+3] > 0 {
			ta = uint32(s[si+3])
			w = ta * fr
			r += uint32(divTable[(uint32(s[si+0])<<8)+ta]) * w
			g += uint32(divTable[(uint32(s[si+1])<<8)+ta]) * w
			b += uint32(divTable[(uint32(s[si+2])<<8)+ta]) * w
			a += w
		}

		if a == 0 {
			d[di+0] = 0
			d[di+1] = 0
			d[di+2] = 0
			d[di+3] = 0
		} else {
			d[di+0] = uint8((r / dlcmlen * 32897) >> 23)
			d[di+1] = uint8((g / dlcmlen * 32897) >> 23)
			d[di+2] = uint8((b / dlcmlen * 32897) >> 23)
			d[di+3] = uint8(a / dlcmlen)
		}
		di += 4
	}
}

// vertColRGBATile performs vertical scaling for a single column in a tile.
func vertColRGBATile(d []byte, s []byte,
	dx, dyStart, dyEnd uint32,
	sx, syStart uint32,
	tt, ft []uint32, slcmlen, dlcmlen uint32,
	dStride, sStride uint32) {

	di := dyStart*dStride + (dx << 2)

	// Initialize fr correctly for starting at dyStart
	fr := uint32(0)
	if dyStart > 0 {
		fr = ft[dyStart-1]
	}

	for dy := dyStart; dy < dyEnd; dy++ {
		tl, tr := tt[dy], tt[dy+1]
		fl := slcmlen - fr
		fr = ft[dy]

		var ta, a, r, g, b, w uint32
		si := (tl - syStart) * sStride + (sx << 2)

		if fl != 0 {
			ta = uint32(s[si+3])
			if ta > 0 {
				w = ta * fl
				r += uint32(divTable[(uint32(s[si+0])<<8)+ta]) * w
				g += uint32(divTable[(uint32(s[si+1])<<8)+ta]) * w
				b += uint32(divTable[(uint32(s[si+2])<<8)+ta]) * w
				a += w
			}
			si += sStride
		}
		for i := tl + 1; i < tr; i++ {
			ta = uint32(s[si+3])
			if ta > 0 {
				w = ta * slcmlen
				r += uint32(divTable[(uint32(s[si+0])<<8)+ta]) * w
				g += uint32(divTable[(uint32(s[si+1])<<8)+ta]) * w
				b += uint32(divTable[(uint32(s[si+2])<<8)+ta]) * w
				a += w
			}
			si += sStride
		}
		if fr != 0 && s[si+3] > 0 {
			ta = uint32(s[si+3])
			w = ta * fr
			r += uint32(divTable[(uint32(s[si+0])<<8)+ta]) * w
			g += uint32(divTable[(uint32(s[si+1])<<8)+ta]) * w
			b += uint32(divTable[(uint32(s[si+2])<<8)+ta]) * w
			a += w
		}

		if a == 0 {
			d[di+0] = 0
			d[di+1] = 0
			d[di+2] = 0
			d[di+3] = 0
		} else {
			d[di+0] = uint8((r / dlcmlen * 32897) >> 23)
			d[di+1] = uint8((g / dlcmlen * 32897) >> 23)
			d[di+2] = uint8((b / dlcmlen * 32897) >> 23)
			d[di+3] = uint8(a / dlcmlen)
		}
		di += dStride
	}
}

// RGBAPartial performs partial downscaling of RGBA images, only processing
// tiles that correspond to dirty source tiles.
// srcTileSize and dstTileSize specify the tile sizes used for dirty tile tracking.
// srcDirtyTiles contains the top-left coordinates of dirty tiles in source coordinates.
func RGBAPartial(ctx context.Context, dest *image.RGBA, src *image.RGBA, srcTileSize, dstTileSize int, srcDirtyTiles []image.Point) error {
	sw, sh := src.Rect.Dx(), src.Rect.Dy()
	dw, dh := dest.Rect.Dx(), dest.Rect.Dy()
	if dw <= 0 || dh <= 0 {
		return nil
	}
	if sw < dw || sh < dh {
		return errors.New("upscale is not supported")
	}
	if len(srcDirtyTiles) == 0 {
		return nil // Nothing changed
	}

	// Calculate which destination tiles need updating
	dstDirtyTiles := calcDstDirtyTiles(sw, sh, dw, dh, srcTileSize, dstTileSize, srcDirtyTiles)
	if len(dstDirtyTiles) == 0 {
		return nil
	}

	var h handle
	h.wg.Add(1)
	go func() {
		defer h.Done()
		tiledRGBAPartial(&h, dest, src, uint32(dstTileSize), dstDirtyTiles)
	}()
	return h.Wait(ctx)
}

func tiledRGBAPartial(parentHandle *handle, dest *image.RGBA, src *image.RGBA, ts uint32, dstDirtyTiles [][2]uint32) {
	sw, sh := uint32(src.Rect.Dx()), uint32(src.Rect.Dy())
	dw, dh := uint32(dest.Rect.Dx()), uint32(dest.Rect.Dy())

	hLcmLen := lcm(sw, dw)
	hSLcmLen, hDLcmLen := hLcmLen/sw, hLcmLen/dw
	hTT, hFT := makeTable(dw, hDLcmLen, hSLcmLen)

	vLcmLen := lcm(sh, dh)
	vSLcmLen, vDLcmLen := vLcmLen/sh, vLcmLen/dh
	vTT, vFT := makeTable(dh, vDLcmLen, vSLcmLen)

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
			processTilesRGBAWithTileSize(parentHandle, tileChan, dest, src, hTT, hFT, vTT, vFT,
				hSLcmLen, hDLcmLen, vSLcmLen, vDLcmLen, sw, dw, sh, dh, ts)
		}()
	}

	wg.Wait()
}

func processTilesRGBAWithTileSize(h *handle, tileChan <-chan [2]uint32,
	dest *image.RGBA, src *image.RGBA,
	hTT, hFT, vTT, vFT []uint32,
	hSLcmLen, hDLcmLen, vSLcmLen, vDLcmLen uint32,
	sw, dw, sh, dh, ts uint32) {

	// Allocate buffer based on tile size
	bufSize := ts * ts * 4 * 4
	buf := make([]byte, bufSize)

	swx4 := sw << 2
	dwx4 := dw << 2

	for tile := range tileChan {
		if h.Aborted() {
			return
		}

		dxStart, dyStart := tile[0], tile[1]
		dxEnd := dxStart + ts
		dyEnd := dyStart + ts
		if dxEnd > dw {
			dxEnd = dw
		}
		if dyEnd > dh {
			dyEnd = dh
		}
		tileW := dxEnd - dxStart

		syStart := vTT[dyStart]
		syEnd := vTT[dyEnd]
		if dyEnd < dh && vFT[dyEnd-1] > 0 {
			syEnd++
		}
		if syEnd > sh {
			syEnd = sh
		}
		srcTileH := syEnd - syStart

		intermediateStride := tileW << 2
		intermediateSize := srcTileH * intermediateStride
		var intermediate []byte
		if int(intermediateSize) <= len(buf) {
			intermediate = buf[:intermediateSize]
		} else {
			intermediate = make([]byte, intermediateSize)
		}

		for sy := syStart; sy < syEnd; sy++ {
			srcRow := src.Pix[sy*swx4:]
			dstRow := intermediate[(sy-syStart)*intermediateStride:]
			horzRowRGBATile(dstRow, srcRow, dxStart, dxEnd, hTT, hFT, hSLcmLen, hDLcmLen)
		}

		for dx := dxStart; dx < dxEnd; dx++ {
			vertColRGBATile(dest.Pix, intermediate,
				dx, dyStart, dyEnd,
				dx-dxStart, syStart,
				vTT, vFT, vSLcmLen, vDLcmLen,
				dwx4, intermediateStride)
		}
	}

	// Suppress unused variable warnings
	_ = swx4
}
