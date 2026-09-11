package render

import (
	"bytes"
	"image/png"
	"testing"
	"time"
)

func TestFormatCountdownGranularity(t *testing.T) {
	d := 2*24*time.Hour + 3*time.Hour + 4*time.Minute + 5*time.Second
	cases := []struct {
		granularity string
		want        string
	}{
		{"seconds", "2d 03:04:05"},
		{"minutes", "2d 03:04"},
		{"hours", "2d 03"},
		{"days", "2d"},
	}
	for _, c := range cases {
		if got := formatCountdown(d, c.granularity); got != c.want {
			t.Errorf("formatCountdown(%v, %q) = %q, want %q", d, c.granularity, got, c.want)
		}
	}

	// Floors finer units rather than rounding up.
	d = 3*time.Hour + 4*time.Minute + 59*time.Second
	if got := formatCountdown(d, "minutes"); got != "03:04" {
		t.Errorf("floor to minutes = %q, want 03:04", got)
	}
	d = 1*24*time.Hour + 5*time.Hour + 30*time.Minute
	if got := formatCountdown(d, "hours"); got != "1d 05" {
		t.Errorf("hours granularity = %q, want 1d 05", got)
	}
	d = 5*24*time.Hour + 12*time.Hour
	if got := formatCountdown(d, "days"); got != "5d" {
		t.Errorf("days granularity = %q, want 5d", got)
	}
	// Larger zero units are omitted; all-zero shows the granularity unit.
	if got := formatCountdown(90*time.Minute, "minutes"); got != "01:30" {
		t.Errorf("90m minutes = %q, want 01:30", got)
	}
	if got := formatCountdown(30*time.Second, "minutes"); got != "00" {
		t.Errorf("30s minutes = %q, want 00", got)
	}
	if got := formatCountdown(30*time.Minute, "hours"); got != "00" {
		t.Errorf("30m hours = %q, want 00", got)
	}
	if got := formatCountdown(12*time.Hour, "days"); got != "0d" {
		t.Errorf("12h days = %q, want 0d", got)
	}
}

func TestCountdownFittingTextFallback(t *testing.T) {
	d := 3*time.Hour + 4*time.Minute + 5*time.Second
	// "03:04:05" is 48px at scale 1; 40px cannot fit, so seconds are dropped.
	if got := countdownFittingText(d, "seconds", 40); got != "03:04" {
		t.Errorf("fallback = %q, want 03:04", got)
	}
	// With enough width the configured granularity is authoritative.
	if got := countdownFittingText(d, "seconds", 1000); got != "03:04:05" {
		t.Errorf("no fallback = %q, want 03:04:05", got)
	}
}

func TestCountdownScale(t *testing.T) {
	if got := countdownScale("00:00", 400, 400); got <= 1 {
		t.Errorf("large device scale = %d, want > 1", got)
	}
	if got := countdownScale("2d 03:04:05", 20, 20); got != 1 {
		t.Errorf("tiny device scale = %d, want 1", got)
	}
}

func TestCountdownDirection(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	opts := DefaultCountdownOptions()
	opts.Granularity = "minutes"

	// Past target counts up.
	past := now.Add(-90 * time.Minute)
	text, ok := countdownState(now.Sub(past), now, 64, opts)
	if !ok || text != "01:30" {
		t.Errorf("count-up past = %q ok=%v, want 01:30", text, ok)
	}

	// Future target clamps to zero, never negative.
	up := CountdownOptions{Granularity: "seconds", Direction: "up"}
	text, ok = countdownState(-time.Hour, now, 64, up)
	if !ok || text != "00:00" {
		t.Errorf("count-up future = %q ok=%v, want 00:00", text, ok)
	}

	// Completion is ignored while counting up.
	up.Completion = "hide"
	up.Granularity = "minutes"
	if _, ok := countdownState(90*time.Minute, now, 64, up); !ok {
		t.Error("completion hide should be ignored for direction=up")
	}
}

func TestCountdownCompletion(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC) // even second
	even := now
	odd := now.Add(time.Second)
	opts := DefaultCountdownOptions()

	// now: built-in Now! with blink.
	text, ok := countdownState(-time.Minute, even, 64, opts)
	if !ok || text != "Now!" {
		t.Errorf("completion now even = %q ok=%v, want Now!", text, ok)
	}
	if _, ok := countdownState(-time.Minute, odd, 64, opts); ok {
		t.Error("completion now odd second should blink off")
	}

	// message: custom text.
	opts.Completion = "message"
	opts.Message = "Doors open"
	text, ok = countdownState(-time.Minute, even, 64, opts)
	if !ok || text != "Doors open" {
		t.Errorf("completion message = %q ok=%v, want Doors open", text, ok)
	}

	// hide: background only.
	opts.Completion = "hide"
	if _, ok := countdownState(-time.Minute, even, 64, opts); ok {
		t.Error("completion hide should draw no text")
	}

	// Completion has no effect before the target.
	opts.Completion = "hide"
	if _, ok := countdownState(time.Minute, even, 64, opts); !ok {
		t.Error("completion should have no effect before the target")
	}
}

func TestRenderCountdownCompletionHideIsBackground(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 1, 0, time.UTC)
	opts := DefaultCountdownOptions()
	opts.Completion = "hide"
	img, err := RenderCountdownWithOptions("", now.Add(-time.Minute), now, 64, 32, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !isBackgroundOnly(t, img.Data) {
		t.Error("hidden completion should render only the background")
	}
}

func TestRenderCountdownLabelOmittedWhenTight(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	target := now.Add(2 * time.Hour)
	withLabel, err := RenderCountdown("Lunch", target, now, 64, 8)
	if err != nil {
		t.Fatal(err)
	}
	noLabel, err := RenderCountdown("", target, now, 64, 8)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(withLabel.Data, noLabel.Data) {
		t.Error("label should be omitted when the canvas is too short")
	}
}

func TestRenderCountdownVerySmallCanvas(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for _, size := range []int{0, 1, 4} {
		img, err := RenderCountdown("", now.Add(time.Hour), now, size, size)
		if err != nil {
			t.Fatalf("RenderCountdown(%d) error: %v", size, err)
		}
		if _, err := png.Decode(bytes.NewReader(img.Data)); err != nil {
			t.Fatalf("png.Decode(%d) error: %v", size, err)
		}
	}
}

func isBackgroundOnly(t *testing.T, data []byte) bool {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("png.Decode error: %v", err)
	}
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if uint8(r>>8) != countdownBG.R || uint8(g>>8) != countdownBG.G || uint8(b>>8) != countdownBG.B {
				return false
			}
		}
	}
	return true
}
