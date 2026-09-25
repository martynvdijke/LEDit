package datasource

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"ledit/render"
)

var _ StateProvider = (*WasteDS)(nil)

// WasteDS fetches waste/bin collection from ICS URL.
// ponytail: naive line scan, no RRULE expansion.
type WasteDS struct {
	Token string
	URL   string
}

// BuildWasteRows parses ICS text looking for BEGIN:VEVENT blocks.
func BuildWasteRows(body []byte) ([][2]string, error) {
	text := string(body)
	// Split by BEGIN:VEVENT
	parts := strings.Split(text, "BEGIN:VEVENT")
	if len(parts) < 2 {
		return nil, fmt.Errorf("waste: no events found")
	}
	var bestSummary, bestDate string
	var bestTime time.Time
	found := false
	for _, block := range parts[1:] {
		// block until END:VEVENT
		if idx := strings.Index(block, "END:VEVENT"); idx >= 0 {
			block = block[:idx]
		}
		var summary, dtstart string
		for _, line := range strings.Split(block, "\n") {
			line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
			if strings.HasPrefix(line, "SUMMARY:") {
				summary = strings.TrimPrefix(line, "SUMMARY:")
			} else if strings.HasPrefix(line, "DTSTART") {
				// DTSTART;VALUE=DATE:20260401 or DTSTART:20260401T...
				if colon := strings.LastIndex(line, ":"); colon >= 0 {
					dtstart = line[colon+1:]
				}
				// trim time suffix
				if len(dtstart) > 8 {
					dtstart = dtstart[:8]
				}
			}
		}
		if summary == "" || dtstart == "" || len(dtstart) < 8 {
			continue
		}
		t, err := time.Parse("20060102", dtstart[:8])
		if err != nil {
			continue
		}
		if !found || t.Before(bestTime) {
			bestTime = t
			bestSummary = summary
			bestDate = t.Format("2006-01-02")
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("waste: no events found")
	}
	if len(bestSummary) > 28 {
		bestSummary = bestSummary[:28]
	}
	return [][2]string{
		{"NEXT", bestSummary},
		{"DATE", bestDate},
	}, nil
}

func fallbackWaste(width, height int) *render.RenderedImage {
	theme := DefaultTheme()
	theme.Title = "WASTE"
	data := map[string]string{"WASTE": "unavailable", "HINT": "set ICS URL"}
	img, _ := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
	return img
}

func (w *WasteDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	if strings.TrimSpace(w.URL) == "" {
		slog.Warn("waste URL empty, using fallback", "source", "waste")
		return fallbackWaste(width, height), nil
	}
	slog.Info("fetching waste ICS", "source", "waste")
	body, err := apiGet(w.URL, "", nil)
	if err != nil {
		slog.Warn("waste API call failed, using fallback", "source", "waste", "error", err)
		return fallbackWaste(width, height), nil
	}
	rows, err := BuildWasteRows(body)
	if err != nil {
		slog.Warn("waste parse failed, using fallback", "source", "waste", "error", err)
		return fallbackWaste(width, height), nil
	}
	data := make(map[string]string, len(rows))
	for _, r := range rows {
		data[r[0]] = r[1]
	}
	theme := DefaultTheme()
	theme.Title = "WASTE"
	img, err := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
	if err != nil {
		return nil, err
	}
	return img, nil
}

func (w *WasteDS) CurrentState(_ context.Context) (map[string]any, error) {
	if strings.TrimSpace(w.URL) == "" {
		return nil, fmt.Errorf("waste: no URL configured")
	}
	body, err := apiGet(w.URL, "", nil)
	if err != nil {
		return nil, err
	}
	rows, err := BuildWasteRows(body)
	if err != nil {
		return nil, err
	}
	var summary, dateStr string
	for _, r := range rows {
		if r[0] == "NEXT" {
			summary = r[1]
		}
		if r[0] == "DATE" {
			dateStr = r[1]
		}
	}
	t, _ := time.Parse("2006-01-02", dateStr)
	now := time.Now().UTC()
	// truncate to date
	nowDate, _ := time.Parse("2006-01-02", now.Format("2006-01-02"))
	daysUntil := int(t.Sub(nowDate).Hours() / 24)
	return map[string]any{
		"next_summary": summary,
		"next_date":    dateStr,
		"days_until":   daysUntil,
	}, nil
}
