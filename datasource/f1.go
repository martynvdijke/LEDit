package datasource

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"ledit/render"
	"ledit/render/themes"
)

type F1DS struct {
	Token string
	URL   string
}

func (f *F1DS) GetPNG() (*render.RenderedImage, error) {
	url := "https://api.openf1.org/v1/race-control?session_key=latest&limit=5"
	if f.URL != "" {
		url = f.URL
	}

	slog.Info("fetching F1 data from openf1", "source", "f1")
	body, err := apiGet(url, f.Token, nil)
	if err == nil {
		var messages []struct {
			Category string `json:"category"`
			Message  string `json:"message"`
			Date     string `json:"date"`
		}
		if err := json.Unmarshal(body, &messages); err == nil && len(messages) > 0 {
			slog.Info("F1 data fetched successfully from openf1", "source", "f1", "count", len(messages))
			data := map[string]string{
				"message": messages[0].Message,
			}
			if len(messages) > 1 {
				data["update"] = messages[1].Message
			}
			return render.RenderDict(data, 400, 400, themes.F1Theme, "fonts/PixelifySans.ttf")
		}
	}

	slog.Warn("F1 openf1 API failed, trying ergast fallback", "source", "f1", "error", err)
	ergastURL := "https://ergast.com/api/f1/current/last/results.json"
	if f.URL != "" {
		ergastURL = f.URL
	}
	body, err = apiGet(ergastURL, "", nil)
	if err == nil {
		var ergast struct {
			MRData struct {
				RaceTable struct {
					Races []struct {
						RaceName string `json:"raceName"`
						Results  []struct {
							Position string `json:"position"`
							Driver   struct {
								GivenName  string `json:"givenName"`
								FamilyName string `json:"familyName"`
							} `json:"Driver"`
							Constructor struct {
								Name string `json:"name"`
							} `json:"Constructor"`
						} `json:"Results"`
					} `json:"Races"`
				} `json:"RaceTable"`
			} `json:"MRData"`
		}
		if err := json.Unmarshal(body, &ergast); err == nil && len(ergast.MRData.RaceTable.Races) > 0 {
			slog.Info("F1 data fetched successfully from ergast", "source", "f1", "race", ergast.MRData.RaceTable.Races[0].RaceName)
			race := ergast.MRData.RaceTable.Races[0]
			data := map[string]string{
				"Next Race": race.RaceName,
			}
			if len(race.Results) > 0 {
				winner := race.Results[0]
				data["Winner"] = fmt.Sprintf("%s %s (%s)", winner.Driver.GivenName, winner.Driver.FamilyName, winner.Constructor.Name)
			}
			return render.RenderDict(data, 400, 400, themes.F1Theme, "fonts/PixelifySans.ttf")
		}
	}

	slog.Warn("F1 all APIs failed, using fallback", "source", "f1")
	return fallbackF1(), nil
}

func fallbackF1() *render.RenderedImage {
	data := map[string]string{
		"Next Race": "No data",
		"Status":    "API unavailable",
	}
	img, _ := render.RenderDict(data, 400, 400, themes.F1Theme, "fonts/PixelifySans.ttf")
	return img
}
