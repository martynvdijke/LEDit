package datasource

import (
	"fmt"
	"log/slog"
	"strings"

	"ledit/render"
)

// GoogleCalendarDS is a thin specialization of the calendar datasource for
// private Google Calendar iCal feeds. No OAuth is required; the private iCal
// URL is enough.
type GoogleCalendarDS struct {
	URL  string
	Name string
}

func (g *GoogleCalendarDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	slog.Info("fetching Google Calendar", "source", "googlecalendar", "url", g.URL)
	body, err := apiGet(g.URL, "", nil)
	if err != nil {
		slog.Warn("google calendar fetch failed, using fallback", "source", "googlecalendar", "error", err)
		return fallbackGoogleCalendar(g.Name, width, height), nil
	}

	events := parseICal(string(body))
	if len(events) == 0 {
		slog.Warn("google calendar no events found, using fallback", "source", "googlecalendar")
		return fallbackGoogleCalendar(g.Name, width, height), nil
	}
	slog.Info("google calendar data fetched successfully", "source", "googlecalendar", "event_count", len(events))

	data := map[string]string{}
	title := "GOOGLE CAL"
	if g.Name != "" {
		title = g.Name
	}
	data["source"] = title

	for i, ev := range events {
		if i >= 4 {
			break
		}
		key := fmt.Sprintf("%d", i+1)
		val := ev
		if len(val) > 28 {
			val = val[:28] + "..."
		}
		data[key] = val
	}

	return render.RenderDict(data, width, height, DefaultTheme(), "fonts/PixelifySans.ttf")
}

func fallbackGoogleCalendar(name string, width, height int) *render.RenderedImage {
	data := map[string]string{
		"source": "GOOGLE CAL",
		"status": "unavailable",
	}
	if name != "" {
		data["source"] = name
	}
	img, _ := render.RenderDict(data, width, height, DefaultTheme(), "fonts/PixelifySans.ttf")
	return img
}

// isGoogleICalURL reports whether the URL looks like a Google Calendar iCal
// feed (private or public).
func isGoogleICalURL(url string) bool {
	return strings.Contains(url, "calendar.google.com/calendar/ical/")
}
