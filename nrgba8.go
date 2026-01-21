package downscale

import (
	"context"
	"errors"
	"image"
	"runtime"
	"sync"
)

var nrgbaTilePool = sync.Pool{
	New: func() any {
		return make([]byte, tileSize*tileSize*4*4)
	},
}

// NRGBA performs cache-friendly tiled downscaling of NRGBA images.
func NRGBA(ctx context.Context, dest *image.NRGBA, src *image.NRGBA) error {
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
		tiledNRGBA(&h, dest, src)
	}()
	return h.Wait(ctx)
}

func tiledNRGBA(parentHandle *handle, dest *image.NRGBA, src *image.NRGBA) {
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
			processTilesNRGBA(parentHandle, tileChan, dest, src, hTT, hFT, vTT, vFT,
				hSLcmLen, hDLcmLen, vSLcmLen, vDLcmLen, sw, dw, sh, dh)
		}()
	}

	// Wait for workers - parent's h.Wait(ctx) handles context cancellation
	// and will call SetAbort(), which workers check via parentHandle.Aborted()
	wg.Wait()
}

func processTilesNRGBA(h *handle, tileChan <-chan [2]uint32,
	dest *image.NRGBA, src *image.NRGBA,
	hTT, hFT, vTT, vFT []uint32,
	hSLcmLen, hDLcmLen, vSLcmLen, vDLcmLen uint32,
	sw, dw, sh, dh uint32) {

	buf := nrgbaTilePool.Get().([]byte)
	defer nrgbaTilePool.Put(buf)

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
		var intermediate []byte
		if int(intermediateSize) <= len(buf) {
			intermediate = buf[:intermediateSize]
		} else {
			intermediate = make([]byte, intermediateSize)
		}

		for sy := syStart; sy < syEnd; sy++ {
			srcRow := src.Pix[sy*swx4:]
			dstRow := intermediate[(sy-syStart)*intermediateStride:]
			horzRowNRGBATile(dstRow, srcRow, dxStart, dxEnd, hTT, hFT, hSLcmLen, hDLcmLen)
		}

		for dx := dxStart; dx < dxEnd; dx++ {
			vertColNRGBATile(dest.Pix, intermediate,
				dx, dyStart, dyEnd,
				dx-dxStart, syStart,
				vTT, vFT, vSLcmLen, vDLcmLen,
				dwx4, intermediateStride)
		}
	}
}

func horzRowNRGBATile(d []byte, s []byte, dxStart, dxEnd uint32, tt, ft []uint32, slcmlen, dlcmlen uint32) {
	di := uint32(0)

	fr := uint32(0)
	if dxStart > 0 {
		fr = ft[dxStart-1]
	}

	for dx := dxStart; dx < dxEnd; dx++ {
		tl, tr := tt[dx], tt[dx+1]
		fl := slcmlen - fr
		fr = ft[dx]

		var a, r, g, b, w uint32
		si := tl << 2

		if fl != 0 {
			w = uint32(s[si+3]) * fl
			r += uint32(s[si+0]) * w
			g += uint32(s[si+1]) * w
			b += uint32(s[si+2]) * w
			a += w
			si += 4
		}
		for i := tl + 1; i < tr; i++ {
			w = uint32(s[si+3]) * slcmlen
			r += uint32(s[si+0]) * w
			g += uint32(s[si+1]) * w
			b += uint32(s[si+2]) * w
			a += w
			si += 4
		}
		if fr != 0 {
			w = uint32(s[si+3]) * fr
			r += uint32(s[si+0]) * w
			g += uint32(s[si+1]) * w
			b += uint32(s[si+2]) * w
			a += w
		}

		if a == 0 {
			d[di+0] = 0
			d[di+1] = 0
			d[di+2] = 0
			d[di+3] = 0
		} else {
			d[di+0] = uint8(r / a)
			d[di+1] = uint8(g / a)
			d[di+2] = uint8(b / a)
			d[di+3] = uint8(a / dlcmlen)
		}
		di += 4
	}
}

func vertColNRGBATile(d []byte, s []byte,
	dx, dyStart, dyEnd uint32,
	sx, syStart uint32,
	tt, ft []uint32, slcmlen, dlcmlen uint32,
	dStride, sStride uint32) {

	di := dyStart*dStride + (dx << 2)

	fr := uint32(0)
	if dyStart > 0 {
		fr = ft[dyStart-1]
	}

	for dy := dyStart; dy < dyEnd; dy++ {
		tl, tr := tt[dy], tt[dy+1]
		fl := slcmlen - fr
		fr = ft[dy]

		var a, r, g, b, w uint32
		si := (tl - syStart) * sStride + (sx << 2)

		if fl != 0 {
			w = uint32(s[si+3]) * fl
			r += uint32(s[si+0]) * w
			g += uint32(s[si+1]) * w
			b += uint32(s[si+2]) * w
			a += w
			si += sStride
		}
		for i := tl + 1; i < tr; i++ {
			w = uint32(s[si+3]) * slcmlen
			r += uint32(s[si+0]) * w
			g += uint32(s[si+1]) * w
			b += uint32(s[si+2]) * w
			a += w
			si += sStride
		}
		if fr != 0 {
			w = uint32(s[si+3]) * fr
			r += uint32(s[si+0]) * w
			g += uint32(s[si+1]) * w
			b += uint32(s[si+2]) * w
			a += w
		}

		if a == 0 {
			d[di+0] = 0
			d[di+1] = 0
			d[di+2] = 0
			d[di+3] = 0
		} else {
			d[di+0] = uint8(r / a)
			d[di+1] = uint8(g / a)
			d[di+2] = uint8(b / a)
			d[di+3] = uint8(a / dlcmlen)
		}
		di += dStride
	}
}
