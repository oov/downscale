package downscale

import (
	"context"
	"errors"
	"image"
	"runtime"
	"sync"
)

type u16NRGBA struct {
	Rect image.Rectangle
	Pix  []uint16
}

var gammaTilePool = sync.Pool{
	New: func() any {
		return make([]uint16, tileSize*tileSize*4*4)
	},
}

// NRGBAGamma performs gamma-corrected downscaling of NRGBA images.
func NRGBAGamma(ctx context.Context, dest *image.NRGBA, src *image.NRGBA, gamma float64) error {
	return NRGBAGammaWithTable(ctx, dest, src, NewGammaTable(gamma))
}

// NRGBAGammaWithTable performs gamma-corrected downscaling using a precomputed gamma table.
// Use this when processing multiple images with the same gamma value to avoid
// repeated table generation.
func NRGBAGammaWithTable(ctx context.Context, dest *image.NRGBA, src *image.NRGBA, table *GammaTable) error {
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

		t8, t16 := table.T8, table.T16
		tmpSrc := &u16NRGBA{
			Pix:  make([]uint16, len(src.Pix)),
			Rect: src.Rect,
		}
		tmpDest := &u16NRGBA{
			Pix:  make([]uint16, len(dest.Pix)),
			Rect: dest.Rect,
		}

		// Convert source to linear space
		{
			s, d := src.Pix, tmpSrc.Pix
			for i := 0; i < len(d); i += 4 {
				d[i+3] = uint16(s[i+3]) * 0x101
				d[i+0] = t8[s[i+0]]
				d[i+1] = t8[s[i+1]]
				d[i+2] = t8[s[i+2]]
			}
			if h.Aborted() {
				return
			}
		}

		tiled16NRGBA(&h, tmpDest, tmpSrc)
		if h.Aborted() {
			return
		}

		// Convert back to gamma space
		{
			s, d := tmpDest.Pix, dest.Pix
			for i := 0; i < len(d); i += 4 {
				d[i+3] = uint8(s[i+3] >> 8)
				d[i+0] = t16[s[i+0]]
				d[i+1] = t16[s[i+1]]
				d[i+2] = t16[s[i+2]]
			}
		}
	}()
	return h.Wait(ctx)
}

// RGBAGamma performs gamma-corrected downscaling of RGBA images.
func RGBAGamma(ctx context.Context, dest *image.RGBA, src *image.RGBA, gamma float64) error {
	return RGBAGammaWithTable(ctx, dest, src, NewGammaTable(gamma))
}

// RGBAGammaWithTable performs gamma-corrected downscaling using a precomputed gamma table.
// Use this when processing multiple images with the same gamma value to avoid
// repeated table generation.
func RGBAGammaWithTable(ctx context.Context, dest *image.RGBA, src *image.RGBA, table *GammaTable) error {
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

		t8, t16 := table.T8, table.T16
		tmpSrc := &u16NRGBA{
			Pix:  make([]uint16, len(src.Pix)),
			Rect: src.Rect,
		}
		tmpDest := &u16NRGBA{
			Pix:  make([]uint16, len(dest.Pix)),
			Rect: dest.Rect,
		}

		// Convert source to linear space (unpremultiply alpha)
		{
			s, d := src.Pix, tmpSrc.Pix
			var a uint32
			for i := 0; i < len(d); i += 4 {
				if a = uint32(s[i+3]); a == 255 {
					d[i+3] = 65535
					d[i+0] = t8[s[i+0]]
					d[i+1] = t8[s[i+1]]
					d[i+2] = t8[s[i+2]]
				} else if a > 0 {
					d[i+3] = uint16(a * 0x101)
					d[i+0] = t8[divTable[(uint32(s[i+0])<<8)+a]]
					d[i+1] = t8[divTable[(uint32(s[i+1])<<8)+a]]
					d[i+2] = t8[divTable[(uint32(s[i+2])<<8)+a]]
				}
			}
			if h.Aborted() {
				return
			}
		}

		tiled16NRGBA(&h, tmpDest, tmpSrc)
		if h.Aborted() {
			return
		}

		// Convert back to gamma space (premultiply alpha)
		{
			s, d := tmpDest.Pix, dest.Pix
			var a uint32
			for i := 0; i < len(d); i += 4 {
				if a = uint32(s[i+3]); a == 65535 {
					d[i+3] = 255
					d[i+0] = t16[s[i+0]]
					d[i+1] = t16[s[i+1]]
					d[i+2] = t16[s[i+2]]
				} else if a == 0 {
					d[i+3] = 0
					d[i+0] = 0
					d[i+1] = 0
					d[i+2] = 0
				} else {
					a >>= 8
					d[i+3] = uint8(a)
					a *= 32897
					d[i+0] = uint8(uint32(t16[s[i+0]]) * a >> 23)
					d[i+1] = uint8(uint32(t16[s[i+1]]) * a >> 23)
					d[i+2] = uint8(uint32(t16[s[i+2]]) * a >> 23)
				}
			}
		}
	}()
	return h.Wait(ctx)
}

func tiled16NRGBA(parentHandle *handle, dest *u16NRGBA, src *u16NRGBA) {
	sw, sh := uint32(src.Rect.Dx()), uint32(src.Rect.Dy())
	dw, dh := uint32(dest.Rect.Dx()), uint32(dest.Rect.Dy())

	hLcmLen := lcm(sw, dw)
	hSLcmLen, hDLcmLen := hLcmLen/sw, hLcmLen/dw
	hTT, hFT := makeTable(dw, hDLcmLen, hSLcmLen)

	vLcmLen := lcm(sh, dh)
	vSLcmLen, vDLcmLen := vLcmLen/sh, vLcmLen/dh
	vTT, vFT := makeTable(dh, vDLcmLen, vSLcmLen)

	numTilesX := int((dw + tileSize - 1) / tileSize)
	numTilesY := int((dh + tileSize - 1) / tileSize)
	totalTiles := numTilesX * numTilesY

	n := runtime.GOMAXPROCS(0)
	if n > totalTiles {
		n = totalTiles
	}

	var wg sync.WaitGroup
	tileChan := make(chan [2]uint32, totalTiles)

	for ty := uint32(0); ty < dh; ty += tileSize {
		for tx := uint32(0); tx < dw; tx += tileSize {
			tileChan <- [2]uint32{tx, ty}
		}
	}
	close(tileChan)

	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			processTiles16NRGBA(parentHandle, tileChan, dest, src, hTT, hFT, vTT, vFT,
				uint64(hSLcmLen), uint64(hDLcmLen), uint64(vSLcmLen), uint64(vDLcmLen), sw, dw, sh, dh)
		}()
	}

	// Wait for workers - parent's h.Wait(ctx) handles context cancellation
	// and will call SetAbort(), which workers check via parentHandle.Aborted()
	wg.Wait()
}

func processTiles16NRGBA(h *handle, tileChan <-chan [2]uint32,
	dest *u16NRGBA, src *u16NRGBA,
	hTT, hFT, vTT, vFT []uint32,
	hSLcmLen, hDLcmLen, vSLcmLen, vDLcmLen uint64,
	sw, dw, sh, dh uint32) {

	buf := gammaTilePool.Get().([]uint16)
	defer gammaTilePool.Put(buf)

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
		var intermediate []uint16
		if int(intermediateSize) <= len(buf) {
			intermediate = buf[:intermediateSize]
		} else {
			intermediate = make([]uint16, intermediateSize)
		}

		for sy := syStart; sy < syEnd; sy++ {
			srcRow := src.Pix[sy*swx4:]
			dstRow := intermediate[(sy-syStart)*intermediateStride:]
			horzRow16NRGBATile(dstRow, srcRow, dxStart, dxEnd, hTT, hFT, hSLcmLen, hDLcmLen)
		}

		for dx := dxStart; dx < dxEnd; dx++ {
			vertCol16NRGBATile(dest.Pix, intermediate,
				dx, dyStart, dyEnd,
				dx-dxStart, syStart,
				vTT, vFT, vSLcmLen, vDLcmLen,
				dwx4, intermediateStride)
		}
	}
}

func horzRow16NRGBATile(d []uint16, s []uint16, dxStart, dxEnd uint32, tt, ft []uint32, slcmlen, dlcmlen uint64) {
	di := uint32(0)

	fr := uint64(0)
	if dxStart > 0 {
		fr = uint64(ft[dxStart-1])
	}

	for dx := dxStart; dx < dxEnd; dx++ {
		tl, tr := tt[dx], tt[dx+1]
		fl := slcmlen - fr
		fr = uint64(ft[dx])

		var a, r, g, b, w uint64
		si := tl << 2

		if fl != 0 {
			w = uint64(s[si+3]) * fl
			r += uint64(s[si+0]) * w
			g += uint64(s[si+1]) * w
			b += uint64(s[si+2]) * w
			a += w
			si += 4
		}
		for i := tl + 1; i < tr; i++ {
			w = uint64(s[si+3]) * slcmlen
			r += uint64(s[si+0]) * w
			g += uint64(s[si+1]) * w
			b += uint64(s[si+2]) * w
			a += w
			si += 4
		}
		if fr != 0 {
			w = uint64(s[si+3]) * fr
			r += uint64(s[si+0]) * w
			g += uint64(s[si+1]) * w
			b += uint64(s[si+2]) * w
			a += w
		}

		if a > 0 {
			d[di+0] = uint16(r / a)
			d[di+1] = uint16(g / a)
			d[di+2] = uint16(b / a)
			d[di+3] = uint16(a / dlcmlen)
		} else {
			d[di+0] = 0
			d[di+1] = 0
			d[di+2] = 0
			d[di+3] = 0
		}
		di += 4
	}
}

func vertCol16NRGBATile(d []uint16, s []uint16,
	dx, dyStart, dyEnd uint32,
	sx, syStart uint32,
	tt, ft []uint32, slcmlen, dlcmlen uint64,
	dStride, sStride uint32) {

	di := dyStart*dStride + (dx << 2)

	fr := uint64(0)
	if dyStart > 0 {
		fr = uint64(ft[dyStart-1])
	}

	for dy := dyStart; dy < dyEnd; dy++ {
		tl, tr := tt[dy], tt[dy+1]
		fl := slcmlen - fr
		fr = uint64(ft[dy])

		var a, r, g, b, w uint64
		si := (tl - syStart) * sStride + (sx << 2)

		if fl != 0 {
			w = uint64(s[si+3]) * fl
			r += uint64(s[si+0]) * w
			g += uint64(s[si+1]) * w
			b += uint64(s[si+2]) * w
			a += w
			si += sStride
		}
		for i := tl + 1; i < tr; i++ {
			w = uint64(s[si+3]) * slcmlen
			r += uint64(s[si+0]) * w
			g += uint64(s[si+1]) * w
			b += uint64(s[si+2]) * w
			a += w
			si += sStride
		}
		if fr != 0 {
			w = uint64(s[si+3]) * fr
			r += uint64(s[si+0]) * w
			g += uint64(s[si+1]) * w
			b += uint64(s[si+2]) * w
			a += w
		}

		if a > 0 {
			d[di+0] = uint16(r / a)
			d[di+1] = uint16(g / a)
			d[di+2] = uint16(b / a)
			d[di+3] = uint16(a / dlcmlen)
		} else {
			d[di+0] = 0
			d[di+1] = 0
			d[di+2] = 0
			d[di+3] = 0
		}
		di += dStride
	}
}
