package datasource

import (
	"encoding/json"
	"log/slog"

	"ledit/render"
)

type HomeAssistantDS struct {
	Token string
	URL   string
}

func (h *HomeAssistantDS) GetPNG() (*render.RenderedImage, error) {
	if h.URL == "" || h.Token == "" {
		slog.Warn("homeassistant not configured", "source", "homeassistant")
		return render.RenderDict(map[string]string{
			"HA Status": "not configured",
		}, 400, 400, DefaultTheme(), "fonts/PixelifySans.ttf")
	}

	slog.Info("fetching homeassistant data", "source", "homeassistant")
	body, err := apiGet(h.URL+"/api/states", h.Token, map[string]string{
		"Authorization": "Bearer " + h.Token,
	})
	if err != nil {
		slog.Warn("homeassistant API call failed, using fallback", "source", "homeassistant", "error", err)
		return fallbackHA(), nil
	}

	var states []struct {
		EntityID string `json:"entity_id"`
		State    string `json:"state"`
	}
	if err := json.Unmarshal(body, &states); err != nil || len(states) == 0 {
		slog.Warn("homeassistant no states in response, using fallback", "source", "homeassistant", "error", err)
		return fallbackHA(), nil
	}

	data := map[string]string{}
	count := 0
	for _, st := range states {
		if count >= 4 {
			break
		}
		if st.State == "" || st.State == "unknown" || st.State == "unavailable" {
			continue
		}
		name := st.EntityID
		if len(name) > 20 {
			name = name[len(name)-20:]
		}
		data[name] = st.State
		count++
	}
	if len(data) == 0 {
		slog.Warn("homeassistant no valid states found, using fallback", "source", "homeassistant")
		return fallbackHA(), nil
	}
	slog.Info("homeassistant data fetched successfully", "source", "homeassistant", "entity_count", count)

	return render.RenderDict(data, 400, 400, DefaultTheme(), "fonts/PixelifySans.ttf")
}

func fallbackHA() *render.RenderedImage {
	data := map[string]string{
		"HA": "unavailable",
	}
	img, _ := render.RenderDict(data, 400, 400, DefaultTheme(), "fonts/PixelifySans.ttf")
	return img
}
