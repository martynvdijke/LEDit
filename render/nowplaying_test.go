package render

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"testing"
)

func decodeRender(t *testing.T, ri *RenderedImage) *image.RGBA {
	t.Helper()
	if ri == nil {
		t.Fatal("nil rendered image")
	}
	m, _, err := image.Decode(bytes.NewReader(ri.Data))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	b := m.Bounds()
	out := image.NewRGBA(b)
	draw.Draw(out, b, m, b.Min, draw.Src)
	return out
}

func TestProgressFraction(t *testing.T) {
	cases := []struct {
		pos, dur int
		want     float64
	}{
		{60, 120, 0.5},
		{0, 120, 0},
		{120, 120, 1},
		{200, 120, 1},
		{-5, 120, 0},
		{10, 0, 0},
		{10, -1, 0},
	}
	for _, c := range cases {
		if got := progressFraction(c.pos, c.dur); got != c.want {
			t.Errorf("progressFraction(%d,%d)=%v want %v", c.pos, c.dur, got, c.want)
		}
	}
}

func TestFormatMMSS(t *testing.T) {
	cases := map[int]string{0: "00:00", 65: "01:05", 200: "03:20", -5: "00:00"}
	for in, want := range cases {
		if got := formatMMSS(in); got != want {
			t.Errorf("formatMMSS(%d)=%q want %q", in, got, want)
		}
	}
}

func TestRenderNowPlayingProgressHalf(t *testing.T) {
	ri, err := RenderNowPlaying(NowPlayingParams{
		Track: "T", Artist: "A", Position: 60, Duration: 120, Width: 64, Height: 32,
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	img := decodeRender(t, ri)
	barY := 31
	accent := color.RGBA{80, 250, 123, 255}
	left := img.RGBAAt(0, barY)
	if left.R != accent.R || left.G != accent.G {
		t.Fatalf("left half not filled: %+v", left)
	}
	if right := img.RGBAAt(63, barY); sameRGB(right, accent) {
		t.Fatalf("right edge unexpectedly filled: %+v", right)
	}
}

func TestRenderNowPlayingZeroDurationOmitsBar(t *testing.T) {
	ri, err := RenderNowPlaying(NowPlayingParams{
		Track: "T", Position: 0, Duration: 0, Width: 64, Height: 32,
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	img := decodeRender(t, ri)
	bg := color.RGBA{40, 42, 54, 255}
	if got := img.RGBAAt(0, 31); !sameRGB(got, bg) {
		t.Fatalf("expected background, got %+v", got)
	}
}

func TestRenderNowPlayingMissingArtFallsBack(t *testing.T) {
	ri, err := RenderNowPlaying(NowPlayingParams{
		Track: "T", Artist: "A", ShowAlbumArt: true,
		ArtURL: "http://127.0.0.1:1/does-not-exist.png", Width: 64, Height: 64,
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	_ = decodeRender(t, ri)
}

func TestRenderNowPlayingTinyCanvas(t *testing.T) {
	ri, err := RenderNowPlaying(NowPlayingParams{
		Track: "Long track name", Artist: "Artist", Duration: 120, Position: 30,
		ShowAlbumArt: true, ArtURL: "http://127.0.0.1:1/x.png", Width: 16, Height: 8,
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	img := decodeRender(t, ri)
	if img.Bounds().Dx() != 16 || img.Bounds().Dy() != 8 {
		t.Fatalf("bad size %v", img.Bounds())
	}
}

func TestNowPlayingLines(t *testing.T) {
	lines := nowPlayingLines(NowPlayingParams{Track: "T", Artist: "A", Album: "Al", Position: 65, Duration: 200})
	if len(lines) != 4 || lines[3] != "01:05 / 03:20" {
		t.Fatalf("unexpected lines: %#v", lines)
	}
	idle := nowPlayingLines(NowPlayingParams{})
	if len(idle) != 1 || idle[0] != "Nothing playing" {
		t.Fatalf("unexpected idle lines: %#v", idle)
	}
}

func sameRGB(a, b color.RGBA) bool {
	return a.R == b.R && a.G == b.G && a.B == b.B
}
