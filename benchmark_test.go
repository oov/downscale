package downscale

import (
	"context"
	"image"
	"image/draw"
	"testing"
)

func BenchmarkRGBA(b *testing.B) {
	s := image.NewRGBA(image.Rect(0, 0, 4000, 3000))
	draw.Draw(s, s.Rect, image.Opaque, image.Point{}, draw.Src)
	d := image.NewRGBA(image.Rect(0, 0, 1222, 1333))
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := RGBA(ctx, d, s); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRGBAGamma(b *testing.B) {
	s := image.NewRGBA(image.Rect(0, 0, 4000, 3000))
	draw.Draw(s, s.Rect, image.Opaque, image.Point{}, draw.Src)
	d := image.NewRGBA(image.Rect(0, 0, 1222, 1333))
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := RGBAGamma(ctx, d, s, 2.2); err != nil {
			b.Fatal(err)
		}
	}
}
