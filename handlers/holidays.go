package handlers

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	holMu        sync.Mutex
	holSet       map[string]bool
	holLastFetch time.Time
	holLastErr   string
)

func SetHolidays(static []string, icsDates []string) {
	holMu.Lock()
	defer holMu.Unlock()
	m := make(map[string]bool)
	for _, d := range static {
		if d != "" {
			m[d] = true
		}
	}
	for _, d := range icsDates {
		if d != "" {
			m[d] = true
		}
	}
	holSet = m
}

func IsHoliday(day time.Time) bool {
	holMu.Lock()
	defer holMu.Unlock()
	if holSet == nil {
		return false
	}
	key := day.Format("2006-01-02")
	return holSet[key]
}

func ResetHolidays() {
	holMu.Lock()
	defer holMu.Unlock()
	holSet = nil
	holLastFetch = time.Time{}
	holLastErr = ""
}

// ParseICSDates extracts VEVENT DTSTART dates; supports DTSTART;VALUE=DATE:YYYYMMDD and DTSTART:YYYYMMDD
func ParseICSDates(body []byte) []string {
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(body))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		// normalize: split at colon
		colon := strings.LastIndex(line, ":")
		if colon < 0 {
			continue
		}
		prefix := line[:colon]
		value := strings.TrimSpace(line[colon+1:])
		if !strings.HasPrefix(prefix, "DTSTART") {
			continue
		}
		// value should be YYYYMMDD or YYYYMMDDTHHMMSSZ etc, take first 8 chars
		// Only handle date-only forms; for datetime, extract date part
		if len(value) < 8 {
			continue
		}
		datePart := value[:8]
		// validate digits
		isDigits := true
		for _, ch := range datePart {
			if ch < '0' || ch > '9' {
				isDigits = false
				break
			}
		}
		if !isDigits {
			continue
		}
		// format YYYY-MM-DD
		formatted := datePart[0:4] + "-" + datePart[4:6] + "-" + datePart[6:8]
		// validate date
		if _, err := time.Parse("2006-01-02", formatted); err != nil {
			continue
		}
		out = append(out, formatted)
	}
	return out
}

func FetchHolidayICS(ctx context.Context, url string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ICS fetch status %d", resp.StatusCode)
	}
	limited := io.LimitReader(resp.Body, 1<<20) // 1MB
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	return ParseICSDates(body), nil
}
