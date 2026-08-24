package datasource

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"ledit/render"
)

type TransitDS struct {
	Token string
	URL   string
}

func (t *TransitDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	url := t.URL
	if url == "" {
		url = "https://v6.vbb.transport.rest/stops/%s/departures"
	}
	if strings.Contains(url, "%s") {
		url = fmt.Sprintf(url, t.Token)
	}

	slog.Info("fetching transit data", "source", "transit", "stop", t.Token)
	body, err := apiGet(url, "", nil)
	if err != nil {
		slog.Warn("transit API call failed, using fallback", "source", "transit", "error", err)
		return fallbackTransit(width, height), nil
	}

	rows, err := BuildTransitRows(body, time.Now())
	if err != nil {
		slog.Warn("transit parse failed, using fallback", "source", "transit", "error", err)
		return fallbackTransit(width, height), nil
	}
	if len(rows) == 0 {
		slog.Warn("transit no departures, using fallback", "source", "transit")
		return fallbackTransit(width, height), nil
	}

	data := map[string]string{
		"title": "TRANSIT",
	}
	for i, r := range rows {
		key := fmt.Sprintf("r%d", i+1)
		// Store as "LINE DEST -> time" combined so RenderDict shows meaningful row.
		// Using r1..r4 keys consistent with fallback convention.
		data[key] = fmt.Sprintf("%s %s", r[0], r[1])
	}

	img, err := render.RenderDict(data, width, height, DefaultTheme(), "fonts/PixelifySans.ttf")
	if err != nil {
		return nil, err
	}
	slog.Info("transit data rendered", "source", "transit", "rows", len(rows))
	return img, nil
}

func fallbackTransit(width, height int) *render.RenderedImage {
	data := map[string]string{
		"title": "TRANSIT",
		"r1":    "unavailable",
	}
	img, _ := render.RenderDict(data, width, height, DefaultTheme(), "fonts/PixelifySans.ttf")
	return img
}

// BuildTransitRows parses departures JSON and returns up to 4 future departures.
// Each row is [2]string{ "LINE DEST" (truncated to 28 chars), "NOW" or "N min" }.
func BuildTransitRows(body []byte, now time.Time) ([][2]string, error) {
	var resp struct {
		Departures []struct {
			Line struct {
				Name string `json:"name"`
			} `json:"line"`
			Destination struct {
				Name string `json:"name"`
			} `json:"destination"`
			PlannedWhen string `json:"plannedWhen"`
			When        string `json:"when"`
		} `json:"departures"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}

	var rows [][2]string
	for _, d := range resp.Departures {
		timeStr := d.When
		if timeStr == "" {
			timeStr = d.PlannedWhen
		}
		if timeStr == "" {
			continue
		}
		t, err := parseTransitTime(timeStr)
		if err != nil {
			continue
		}
		if !t.After(now) {
			continue
		}
		diff := t.Sub(now)
		mins := int(math.Round(diff.Minutes()))
		var row2 string
		if mins <= 1 {
			row2 = "NOW"
		} else {
			row2 = fmt.Sprintf("%d min", mins)
		}
		line := strings.TrimSpace(d.Line.Name)
		dest := strings.TrimSpace(d.Destination.Name)
		row1 := strings.TrimSpace(line + " " + dest)
		row1 = strings.TrimSpace(row1)
		if len(row1) > 28 {
			row1 = row1[:28]
		}
		if row1 == "" {
			row1 = "?"
		}
		rows = append(rows, [2]string{row1, row2})
		if len(rows) >= 4 {
			break
		}
	}
	if rows == nil {
		rows = [][2]string{}
	}
	return rows, nil
}

func parseTransitTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}
