package downscale

import (
	"context"
	"image"
	"runtime"
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

func nn(ctx context.Context, dPix []byte, sPix []byte, dw int, dh int, sw int, sh int) error {
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

func nnInner(h *handle, yMin int, yMax int, dPix []byte, sPix []byte, dw int, dh int, sw int, sh int) {
	defer h.Done()
	mx := float32(sw) / float32(dw)
	my := float32(sh) / float32(dh)
	dwx4, swx4 := dw<<2, sw<<2
	for dy := yMin; dy < yMax; dy++ {
		if dy&7 == 7 && h.Aborted() {
			return
		}
		s := sPix[int(float32(dy)*my+0.5)*swx4:]
		d := dPix[dy*dwx4:]
		for dx, sx := (0), (0); dx < dwx4; dx += 4 {
			sx = int(float32(dx>>2)*mx+0.5) << 2
			d[dx+3] = s[sx+3]
			d[dx+2] = s[sx+2]
			d[dx+1] = s[sx+1]
			d[dx+0] = s[sx+0]
		}
	}
}
