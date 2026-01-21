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

func BenchmarkNRGBA(b *testing.B) {
	s := image.NewNRGBA(image.Rect(0, 0, 4000, 3000))
	draw.Draw(s, s.Rect, image.Opaque, image.Point{}, draw.Src)
	d := image.NewNRGBA(image.Rect(0, 0, 1222, 1333))
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := NRGBA(ctx, d, s); err != nil {
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

func BenchmarkRGBAGammaWithTable(b *testing.B) {
	s := image.NewRGBA(image.Rect(0, 0, 4000, 3000))
	draw.Draw(s, s.Rect, image.Opaque, image.Point{}, draw.Src)
	d := image.NewRGBA(image.Rect(0, 0, 1222, 1333))
	ctx := context.Background()
	table := NewGammaTable(2.2)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := RGBAGammaWithTable(ctx, d, s, table); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMakeTable(b *testing.B) {
	testData := makeTableTestData[0]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lcmlen := lcm(testData.sw, testData.dw)
		slcmlen, dlcmlen := lcmlen/testData.sw, lcmlen/testData.dw
		makeTable(testData.dw, dlcmlen, slcmlen)
	}
}

func BenchmarkRGBAFast(b *testing.B) {
	s := image.NewRGBA(image.Rect(0, 0, 4000, 3000))
	draw.Draw(s, s.Rect, image.Opaque, image.Point{}, draw.Src)
	d := image.NewRGBA(image.Rect(0, 0, 1222, 1333))
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := RGBAFast(ctx, d, s); err != nil {
			b.Fatal(err)
		}
	}
}
