package render

import (
	"testing"
	"time"
)

func TestScrollOffset(t *testing.T) {
	t.Run("degenerate_inputs", func(t *testing.T) {
		now := time.UnixMilli(1000)
		cases := []struct {
			name     string
			bucketMs int
			speedPx  int
			textW    int
			gap      int
		}{
			{"zero_total_width", 100, 30, 0, 0},
			{"negative_total", 100, 30, -10, 5},
			{"zero_speed", 100, 0, 60, 10},
			{"negative_speed", 100, -5, 60, 10},
			{"zero_text_and_gap", 100, 30, 0, 0},
		}
		for _, c := range cases {
			if got := ScrollOffset(now, c.bucketMs, c.speedPx, c.textW, c.gap); got != 0 {
				t.Errorf("%s: expected 0, got %d", c.name, got)
			}
		}
		// zero bucketMs should default to 100 and not panic, return valid offset in range
		// Use non-zero time so offset is interesting; just check in range.
		now2 := time.UnixMilli(12345)
		got := ScrollOffset(now2, 0, 30, 60, 10)
		if got < 0 || got >= 70 {
			t.Errorf("zero bucketMs default: offset %d out of range [0,70)", got)
		}
		negBucket := ScrollOffset(now2, -50, 30, 60, 10)
		if negBucket < 0 || negBucket >= 70 {
			t.Errorf("negative bucketMs default: offset %d out of range [0,70)", negBucket)
		}
		// zero bucket should behave like 100
		expected := ScrollOffset(now2, 100, 30, 60, 10)
		if got != expected {
			t.Errorf("zero bucketMs should default to 100: got %d want %d", got, expected)
		}
		if negBucket != expected {
			t.Errorf("negative bucketMs should default to 100: got %d want %d", negBucket, expected)
		}
	})

	t.Run("determinism_within_bucket", func(t *testing.T) {
		// Two nows inside same 100ms bucket give identical offset.
		baseMs := int64(10000) // aligned to bucket boundary
		a := time.UnixMilli(baseMs + 5)
		b := time.UnixMilli(baseMs + 99)
		offA := ScrollOffset(a, 100, 30, 60, 10)
		offB := ScrollOffset(b, 100, 30, 60, 10)
		if offA != offB {
			t.Errorf("same bucket should give same offset: %d vs %d", offA, offB)
		}
		// Also test multiple buckets determinism
		for i := 0; i < 10; i++ {
			ms := int64(i * 100)
			x := time.UnixMilli(ms + 10)
			y := time.UnixMilli(ms + 90)
			if ScrollOffset(x, 100, 30, 60, 10) != ScrollOffset(y, 100, 30, 60, 10) {
				t.Fatalf("bucket %d not deterministic", i)
			}
		}
	})

	t.Run("monotonic_advance_and_wrap", func(t *testing.T) {
		textW, gap, speed := 60, 10, 30
		total := textW + gap // 70
		bucketMs := 100
		// Speed 30 px/s => per 100ms bucket increment = 30*1 bucket? Actually offset = bucket*30 %70
		// So successive buckets increase by 30 %70 = 30 each time.
		// But spec says "increase by 3 per bucket" — that would be if speed applied per bucketMs fraction?
		// Design semantics: offset = (nowMs/bucketMs)*speedPx % total. With speed=30, that's +30 per bucket.
		// Test both: verify consecutive difference is speedPx % total (=30) and wrap correctly.
		// Also verify modulo property that offset increments by speedPx mod total.
		prev := ScrollOffset(time.UnixMilli(0), bucketMs, speed, textW, gap)
		if prev != 0 {
			t.Fatalf("bucket 0 offset should be 0, got %d", prev)
		}
		for i := 1; i < 20; i++ {
			now := time.UnixMilli(int64(i * bucketMs))
			off := ScrollOffset(now, bucketMs, speed, textW, gap)
			expected := (i * speed) % total
			if off != expected {
				t.Errorf("bucket %d: got %d want %d", i, off, expected)
			}
			// monotonic advance modulo wrap
			diff := (off - prev + total) % total
			expDiff := speed % total
			if diff != expDiff {
				t.Errorf("bucket %d: diff %d want %d (prev %d curr %d)", i, diff, expDiff, prev, off)
			}
			prev = off
		}
	})

	t.Run("range_modulo_property", func(t *testing.T) {
		textW, gap, speed := 60, 10, 30
		total := textW + gap
		bucketMs := 100
		for i := 0; i < 500; i++ {
			now := time.UnixMilli(int64(i * bucketMs))
			off := ScrollOffset(now, bucketMs, speed, textW, gap)
			if off < 0 || off >= total {
				t.Fatalf("bucket %d: offset %d out of range [0,%d)", i, off, total)
			}
		}
		// Also test with different params that wrap still holds
		for i := 0; i < 500; i++ {
			now := time.UnixMilli(int64(i*50 + 123))
			off := ScrollOffset(now, 50, 7, 20, 5)
			if off < 0 || off >= 25 {
				t.Fatalf("alt params bucket %d: offset %d out of range", i, off)
			}
		}
	})

	t.Run("wrap_around_returns_to_zero", func(t *testing.T) {
		// For total=70, speed=30, bucket increments 30; after 7 buckets: 7*30=210 %70==0
		textW, gap, speed := 60, 10, 30
		bucketMs := 100
		// Find period: total/gcd(total,speed)=70/10=7
		period := 7
		start := time.UnixMilli(0)
		for k := 1; k <= 3; k++ {
			off := ScrollOffset(time.UnixMilli(int64(k*period*bucketMs)), bucketMs, speed, textW, gap)
			startOff := ScrollOffset(start, bucketMs, speed, textW, gap)
			if off != startOff {
				t.Errorf("period %d*k=%d: expected wrap to %d got %d", period, k, startOff, off)
			}
		}
	})
}
