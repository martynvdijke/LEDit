package render

import "time"

// ScrollOffset returns the x pixel offset for a scrolling marquee row.
// bucketMs is the time bucket (100ms); speedPx is pixels per second;
// totalWidth = textPixelWidth + gap. Deterministic: same bucket -> same offset.
func ScrollOffset(now time.Time, bucketMs, speedPx, textWidth, gap int) int {
	if bucketMs <= 0 {
		bucketMs = 100
	}
	if speedPx <= 0 {
		return 0
	}
	total := textWidth + gap
	if total <= 0 {
		return 0
	}
	bucket := now.UnixMilli() / int64(bucketMs)
	offset := (bucket * int64(speedPx)) % int64(total)
	return int(offset)
}
