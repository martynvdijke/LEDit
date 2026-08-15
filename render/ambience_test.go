package render

import (
	"bytes"
	"image/png"
	"math"
	"testing"
	"time"
)

func TestClockAngles(t *testing.T) {
	cases := []struct {
		now  time.Time
		hour float64
		min  float64
		sec  float64
	}{
		{time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), 0, 0, 0},
		{time.Date(2026, 1, 1, 3, 0, 0, 0, time.UTC), math.Pi / 2, 0, 0},
		{time.Date(2026, 1, 1, 0, 15, 0, 0, time.UTC), 15.0 / 60.0 * math.Pi / 6, math.Pi / 2, 0},
		{time.Date(2026, 1, 1, 0, 0, 30, 0, time.UTC), 30.0 / 3600.0 * math.Pi / 6, 30.0 / 60.0 * math.Pi / 30, math.Pi},
		{time.Date(2026, 1, 1, 12, 30, 0, 0, time.UTC), 30.0 / 60.0 * math.Pi / 6, math.Pi, 0},
	}
	for _, c := range cases {
		hour, min, sec := clockAngles(c.now)
		if math.Abs(hour-c.hour) > 1e-9 {
			t.Errorf("hour angle for %v: got %v want %v", c.now, hour, c.hour)
		}
		if math.Abs(min-c.min) > 1e-9 {
			t.Errorf("minute angle for %v: got %v want %v", c.now, min, c.min)
		}
		if math.Abs(sec-c.sec) > 1e-9 {
			t.Errorf("second angle for %v: got %v want %v", c.now, sec, c.sec)
		}
	}
}

func TestRenderAnalogClockSizes(t *testing.T) {
	now := time.Date(2026, 1, 1, 10, 15, 30, 0, time.UTC)
	for _, size := range []int{32, 64, 128} {
		img, err := RenderAnalogClock(now, size, size)
		if err != nil {
			t.Fatalf("RenderAnalogClock(%d) error: %v", size, err)
		}
		if img.Format != "PNG" {
			t.Errorf("format = %q, want PNG", img.Format)
		}
		decoded, err := png.Decode(bytes.NewReader(img.Data))
		if err != nil {
			t.Fatalf("png.Decode(%d) error: %v", size, err)
		}
		if got := decoded.Bounds().Dx(); got != size {
			t.Errorf("width = %d, want %d", got, size)
		}
		if got := decoded.Bounds().Dy(); got != size {
			t.Errorf("height = %d, want %d", got, size)
		}
	}
}

func TestRenderAnalogClockDeterministic(t *testing.T) {
	now := time.Date(2026, 1, 1, 10, 15, 30, 0, time.UTC)
	a, err := RenderAnalogClock(now, 64, 64)
	if err != nil {
		t.Fatal(err)
	}
	b, err := RenderAnalogClock(now, 64, 64)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Data, b.Data) {
		t.Error("same time produced different PNG bytes")
	}
}

func TestRenderAnalogClockAdvances(t *testing.T) {
	a, err := RenderAnalogClock(time.Date(2026, 1, 1, 10, 15, 30, 0, time.UTC), 64, 64)
	if err != nil {
		t.Fatal(err)
	}
	b, err := RenderAnalogClock(time.Date(2026, 1, 1, 10, 16, 30, 0, time.UTC), 64, 64)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a.Data, b.Data) {
		t.Error("different times produced identical PNG bytes")
	}
}

func TestRenderMatrixRainDeterministic(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	a, err := RenderMatrixRain(now, 64, 32)
	if err != nil {
		t.Fatal(err)
	}
	b, err := RenderMatrixRain(now, 64, 32)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Data, b.Data) {
		t.Error("same time produced different PNG bytes")
	}
	if a.Format != "PNG" {
		t.Errorf("format = %q, want PNG", a.Format)
	}
}

func TestRenderMatrixRainAdvances(t *testing.T) {
	a, err := RenderMatrixRain(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), 64, 32)
	if err != nil {
		t.Fatal(err)
	}
	b, err := RenderMatrixRain(time.Date(2026, 1, 1, 12, 0, 1, 0, time.UTC), 64, 32)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a.Data, b.Data) {
		t.Error("different times produced identical PNG bytes")
	}
}

func TestRenderMatrixRainSizes(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for _, size := range []int{32, 64, 128, 400} {
		img, err := RenderMatrixRain(now, size, size)
		if err != nil {
			t.Fatalf("RenderMatrixRain(%d) error: %v", size, err)
		}
		if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
			t.Fatalf("png.Decode(%d) error: %v", size, err)
		}
	}
}

func TestFormatCountdown(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{25 * time.Hour, "1d 01:00:00"},
		{24 * time.Hour, "1d 00:00:00"},
		{23 * time.Hour, "23:00:00"},
		{1 * time.Hour, "01:00:00"},
		{59*time.Minute + 59*time.Second, "59:59"},
		{1 * time.Minute, "01:00"},
		{0, "DONE"},
		{-5 * time.Second, "DONE"},
	}
	for _, c := range cases {
		if got := formatCountdown(c.d); got != c.want {
			t.Errorf("formatCountdown(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestRenderCountdown(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	target := now.Add(2*time.Hour + 30*time.Minute)
	img, err := RenderCountdown("Lunch", target, now, 64, 32)
	if err != nil {
		t.Fatal(err)
	}
	if img.Format != "PNG" {
		t.Errorf("format = %q, want PNG", img.Format)
	}
	if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
		t.Fatalf("png.Decode error: %v", err)
	}

	// DONE state blinks: even second shows DONE, odd second blank.
	done := now.Add(-time.Minute)
	on, err := RenderCountdown("", done, now.Add(2*time.Second), 64, 32)
	if err != nil {
		t.Fatal(err)
	}
	off, err := RenderCountdown("", done, now.Add(3*time.Second), 64, 32)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(on.Data, off.Data) {
		t.Error("DONE blink should alternate blank/content frames")
	}
}

func BenchmarkRenderAmbience(b *testing.B) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	target := now.Add(3 * time.Hour)
	for _, size := range []int{32, 64, 400} {
		b.Run("clock", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := RenderAnalogClock(now, size, size); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run("rain", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := RenderMatrixRain(now, size, size); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run("countdown", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := RenderCountdown("Lunch", target, now, size, size/2); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
