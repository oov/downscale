package downscale

import (
	"context"
	"errors"
	"image"
	"runtime"
)

type u16NRGBA struct {
	Rect image.Rectangle
	Pix  []uint16
}

func NRGBAGamma(ctx context.Context, dest *image.NRGBA, src *image.NRGBA, gamma float64) error {
	sw, sh := src.Rect.Dx(), src.Rect.Dy()
	dw, dh := dest.Rect.Dx(), dest.Rect.Dy()
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

		t8, t16 := makeGammaTable(gamma)
		tmpSrc := &u16NRGBA{
			Pix:  make([]uint16, len(src.Pix)),
			Rect: src.Rect,
		}
		tmpDest := &u16NRGBA{
			Pix:  make([]uint16, len(dest.Pix)),
			Rect: dest.Rect,
		}

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

		if sh != dh {
			if sw != dw {
				tmp := &u16NRGBA{
					Pix:  make([]uint16, (dw<<2)*sh),
					Rect: image.Rect(0, 0, dw, sh),
				}
				horz16NRGBA(ctx, tmp, tmpSrc)
				if h.Aborted() {
					return
				}
				vert16NRGBA(ctx, tmpDest, tmp)
			} else {
				vert16NRGBA(ctx, tmpDest, tmpSrc)
			}
		} else {
			horz16NRGBA(ctx, tmpDest, tmpSrc)
		}
		if h.Aborted() {
			return
		}

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

func RGBAGamma(ctx context.Context, dest *image.RGBA, src *image.RGBA, gamma float64) error {
	sw, sh := src.Rect.Dx(), src.Rect.Dy()
	dw, dh := dest.Rect.Dx(), dest.Rect.Dy()
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

		t8, t16 := makeGammaTable(gamma)
		tmpSrc := &u16NRGBA{
			Pix:  make([]uint16, len(src.Pix)),
			Rect: src.Rect,
		}
		tmpDest := &u16NRGBA{
			Pix:  make([]uint16, len(dest.Pix)),
			Rect: dest.Rect,
		}

		{
			s, d := src.Pix, tmpSrc.Pix
			var a uint32
			for i := 0; i < len(d); i += 4 {
				if a = uint32(s[i+3]); a > 0 {
					d[i+3] = uint16(a * 0x101)
					d[i+0] = t8[uint32(s[i+0])*255/a]
					d[i+1] = t8[uint32(s[i+1])*255/a]
					d[i+2] = t8[uint32(s[i+2])*255/a]
				}
			}
			if h.Aborted() {
				return
			}
		}

		if sh != dh {
			if sw != dw {
				tmp := &u16NRGBA{
					Pix:  make([]uint16, (dw<<2)*sh),
					Rect: image.Rect(0, 0, dw, sh),
				}
				horz16NRGBA(ctx, tmp, tmpSrc)
				if h.Aborted() {
					return
				}
				vert16NRGBA(ctx, tmpDest, tmp)
			} else {
				vert16NRGBA(ctx, tmpDest, tmpSrc)
			}
		} else {
			horz16NRGBA(ctx, tmpDest, tmpSrc)
		}
		if h.Aborted() {
			return
		}

		{
			s, d := tmpDest.Pix, dest.Pix
			var a uint32
			for i := 0; i < len(d); i += 4 {
				if s[i+3] > 0 {
					a = uint32(s[i+3]) >> 8
					d[i+3] = uint8(a)
					a *= 32897
					d[i+0] = uint8(uint32(t16[s[i+0]]) * a >> 23)
					d[i+1] = uint8(uint32(t16[s[i+1]]) * a >> 23)
					d[i+2] = uint8(uint32(t16[s[i+2]]) * a >> 23)
				} else {
					d[i+3] = 0
					d[i+0] = 0
					d[i+1] = 0
					d[i+2] = 0
				}
			}
		}
	}()
	return h.Wait(ctx)
}

func horz16NRGBA(ctx context.Context, dest *u16NRGBA, src *u16NRGBA) error {
	n := runtime.GOMAXPROCS(0)
	for n > 1 && n<<1 > dest.Rect.Dy() {
		n--
	}

	sw, dw := uint32(src.Rect.Dx()), uint32(dest.Rect.Dx())
	lcmlen := lcm(sw, dw)
	slcmlen, dlcmlen := lcmlen/sw, lcmlen/dw
	tt, ft := makeTable(dw, dlcmlen, slcmlen)
	dh := uint32(dest.Rect.Dy())

	h := &handle{}
	h.wg.Add(n)
	step := dh / uint32(n)
	y := uint32(0)
	for i := 1; i < n; i++ {
		go horz16NRGBAInner(h, src.Pix, dest.Pix, y, y+step, uint64(slcmlen), uint64(dlcmlen), sw, dw, tt, ft)
		y += step
	}
	go horz16NRGBAInner(h, src.Pix, dest.Pix, y, dh, uint64(slcmlen), uint64(dlcmlen), sw, dw, tt, ft)
	return h.Wait(ctx)
}

func vert16NRGBA(ctx context.Context, dest *u16NRGBA, src *u16NRGBA) error {
	n := runtime.GOMAXPROCS(0)
	for n > 1 && n<<1 > dest.Rect.Dx() {
		n--
	}

	sw, dw := uint32(src.Rect.Dx()), uint32(dest.Rect.Dx())
	sh, dh := uint32(src.Rect.Dy()), uint32(dest.Rect.Dy())
	lcmlen := lcm(sh, dh)
	slcmlen, dlcmlen := lcmlen/sh, lcmlen/dh
	tt, ft := makeTable(dh, dlcmlen, slcmlen)

	h := &handle{}
	h.wg.Add(n)
	step := (dw / uint32(n)) << 2
	x := uint32(0)
	for i := 1; i < n; i++ {
		go vert16NRGBAInner(h, src.Pix, dest.Pix, x, x+step, uint64(slcmlen), uint64(dlcmlen), sw, dw, dh, tt, ft)
		x += step
	}
	go vert16NRGBAInner(h, src.Pix, dest.Pix, x, dw<<2, uint64(slcmlen), uint64(dlcmlen), sw, dw, dh, tt, ft)
	return h.Wait(ctx)
}

func horz16NRGBAInner(h *handle, s []uint16, d []uint16, yMin uint32, yMax uint32, slcmlen uint64, dlcmlen uint64, sw uint32, dw uint32, tt []uint32, ft []uint32) {
	defer h.Done()
	swx4, dwx4 := sw<<2, dw<<2
	for y := yMin; y < yMax; y++ {
		if y&7 == 7 && h.Aborted() {
			return
		}
		di := y * dwx4
		si := y * swx4
		for x, fr := uint32(0), uint64(0); x < dw; x++ {
			tl, tr := tt[x], tt[x+1]
			fl := uint64(slcmlen) - fr
			fr = uint64(ft[x])
			var a, r, g, b, w uint64
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
			if a == 0 {
				d[di+0] = 0
				d[di+1] = 0
				d[di+2] = 0
				d[di+3] = 0
			} else {
				d[di+0] = uint16(r / a)
				d[di+1] = uint16(g / a)
				d[di+2] = uint16(b / a)
				d[di+3] = uint16(a / dlcmlen)
			}
			di += 4
		}
	}
}

func vert16NRGBAInner(h *handle, s []uint16, d []uint16, xMin uint32, xMax uint32, slcmlen uint64, dlcmlen uint64, sw uint32, dw uint32, dh uint32, tt []uint32, ft []uint32) {
	defer h.Done()
	swx4, dwx4 := sw<<2, dw<<2
	for x := xMin; x < xMax; x += 4 {
		if (x>>2)&7 == 7 && h.Aborted() {
			return
		}
		di, si := x, x
		for y, fr := uint32(0), uint64(0); y < dh; y++ {
			tl, tr := tt[y], tt[y+1]
			fl := slcmlen - fr
			fr = uint64(ft[y])
			var a, r, g, b, w uint64
			if fl != 0 {
				w = uint64(s[si+3]) * fl
				r += uint64(s[si+0]) * w
				g += uint64(s[si+1]) * w
				b += uint64(s[si+2]) * w
				a += w
				si += swx4
			}
			for i := tl + 1; i < tr; i++ {
				w = uint64(s[si+3]) * slcmlen
				r += uint64(s[si+0]) * w
				g += uint64(s[si+1]) * w
				b += uint64(s[si+2]) * w
				a += w
				si += swx4
			}
			if fr != 0 {
				w = uint64(s[si+3]) * fr
				r += uint64(s[si+0]) * w
				g += uint64(s[si+1]) * w
				b += uint64(s[si+2]) * w
				a += w
			}
			if a == 0 {
				d[di+0] = 0
				d[di+1] = 0
				d[di+2] = 0
				d[di+3] = 0
			} else {
				d[di+0] = uint16(r / a)
				d[di+1] = uint16(g / a)
				d[di+2] = uint16(b / a)
				d[di+3] = uint16(a / dlcmlen)
			}
			di += dwx4
		}
	}
}
