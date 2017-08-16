//go:generate go run gentable.go

package downscale

import (
	"context"
	"errors"
	"image"
	"runtime"
)

func RGBA(ctx context.Context, dest *image.RGBA, src *image.RGBA) error {
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
		if sh != dh {
			if sw != dw {
				tmp := image.NewRGBA(image.Rect(0, 0, dw, sh))
				horz8RGBA(ctx, tmp, src)
				if h.Aborted() {
					return
				}
				vert8RGBA(ctx, dest, tmp)
			} else {
				vert8RGBA(ctx, dest, src)
			}
		} else {
			horz8RGBA(ctx, dest, src)
		}
	}()
	return h.Wait(ctx)
}

func horz8RGBA(ctx context.Context, dest *image.RGBA, src *image.RGBA) error {
	n := runtime.GOMAXPROCS(0)
	for n > 1 && n<<1 > dest.Rect.Dy() {
		n--
	}

	sw, dw := uint32(src.Rect.Dx()), uint32(dest.Rect.Dx())
	lcmlen := lcm(sw, dw)
	slcmlen, dlcmlen := lcmlen/sw, lcmlen/dw
	tt, ft := makeTable(dw, dlcmlen, slcmlen)
	dh := uint32(dest.Rect.Dy())

	var h handle
	h.wg.Add(n)
	step := dh / uint32(n)
	y := uint32(0)
	for i := 1; i < n; i++ {
		go horz8RGBAInner(&h, y, y+step, dest.Pix, src.Pix, dlcmlen, slcmlen, dw, sw, tt, ft)
		y += step
	}
	go horz8RGBAInner(&h, y, dh, dest.Pix, src.Pix, dlcmlen, slcmlen, dw, sw, tt, ft)
	return h.Wait(ctx)
}

func vert8RGBA(ctx context.Context, dest *image.RGBA, src *image.RGBA) error {
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
		go vert8RGBAInner(h, x, x+step, dest.Pix, src.Pix, dlcmlen, slcmlen, dw, dh, sw, tt, ft)
		x += step
	}
	go vert8RGBAInner(h, x, dw<<2, dest.Pix, src.Pix, dlcmlen, slcmlen, dw, dh, sw, tt, ft)
	return h.Wait(ctx)
}

func horz8RGBAInner(h *handle, yMin uint32, yMax uint32, d []byte, s []byte, dlcmlen uint32, slcmlen uint32, dw uint32, sw uint32, tt []uint32, ft []uint32) {
	defer h.Done()
	swx4, dwx4 := sw<<2, dw<<2
	for y := yMin; y < yMax; y++ {
		if y&7 == 7 && h.Aborted() {
			return
		}
		di := y * dwx4
		si := y * swx4
		for x, fr := uint32(0), uint32(0); x < dw; x++ {
			tl, tr := tt[x], tt[x+1]
			fl := slcmlen - fr
			fr = ft[x]
			var ta, a, r, g, b, w uint32
			if fl != 0 {
				ta = uint32(s[si+3])
				if ta > 0 {
					w = ta * fl
					r += divTable[(uint32(s[si+0])<<8)+ta] * w
					g += divTable[(uint32(s[si+1])<<8)+ta] * w
					b += divTable[(uint32(s[si+2])<<8)+ta] * w
					a += w
				}
				si += 4
			}
			for i := tl + 1; i < tr; i++ {
				ta = uint32(s[si+3])
				if ta > 0 {
					w = ta * slcmlen
					r += divTable[(uint32(s[si+0])<<8)+ta] * w
					g += divTable[(uint32(s[si+1])<<8)+ta] * w
					b += divTable[(uint32(s[si+2])<<8)+ta] * w
					a += w
				}
				si += 4
			}
			if fr != 0 && s[si+3] > 0 {
				ta = uint32(s[si+3])
				w = ta * fr
				r += divTable[(uint32(s[si+0])<<8)+ta] * w
				g += divTable[(uint32(s[si+1])<<8)+ta] * w
				b += divTable[(uint32(s[si+2])<<8)+ta] * w
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
}

func vert8RGBAInner(h *handle, xMin uint32, xMax uint32, d []byte, s []byte, dlcmlen uint32, slcmlen uint32, dw uint32, dh uint32, sw uint32, tt []uint32, ft []uint32) {
	defer h.Done()
	swx4, dwx4 := sw<<2, dw<<2
	for x := xMin; x < xMax; x += 4 {
		if (x>>2)&7 == 7 && h.Aborted() {
			return
		}
		di, si := x, x
		for y, fr := uint32(0), uint32(0); y < dh; y++ {
			tl, tr := tt[y], tt[y+1]
			fl := slcmlen - fr
			fr = ft[y]
			var ta, a, r, g, b, w uint32
			if fl != 0 {
				ta = uint32(s[si+3])
				if ta > 0 {
					w = ta * fl
					r += divTable[(uint32(s[si+0])<<8)+ta] * w
					g += divTable[(uint32(s[si+1])<<8)+ta] * w
					b += divTable[(uint32(s[si+2])<<8)+ta] * w
					a += w
				}
				si += swx4
			}
			for i := tl + 1; i < tr; i++ {
				ta = uint32(s[si+3])
				if ta > 0 {
					w = ta * slcmlen
					r += divTable[(uint32(s[si+0])<<8)+ta] * w
					g += divTable[(uint32(s[si+1])<<8)+ta] * w
					b += divTable[(uint32(s[si+2])<<8)+ta] * w
					a += w
				}
				si += swx4
			}
			if fr != 0 && s[si+3] > 0 {
				ta = uint32(s[si+3])
				w = ta * fr
				r += divTable[(uint32(s[si+0])<<8)+ta] * w
				g += divTable[(uint32(s[si+1])<<8)+ta] * w
				b += divTable[(uint32(s[si+2])<<8)+ta] * w
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
			di += dwx4
		}
	}
}
