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
