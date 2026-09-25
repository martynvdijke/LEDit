package datasource

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"ledit/render"
	"ledit/render/themes"
)

var _ StateProvider = (*UntappdDS)(nil)

type UntappdDS struct {
	Token string
	URL   string
}

func (u *UntappdDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	if strings.TrimSpace(u.Token) == "" && strings.TrimSpace(u.URL) == "" {
		slog.Debug("using mock untappd data", "source", "untappd")
		return renderUntappdMock(width, height), nil
	}
	url := u.buildURL()
	slog.Debug("fetching untappd data", "source", "untappd")
	body, err := apiGet(url, "", nil)
	if err != nil {
		slog.Warn("untappd API call failed, using fallback", "source", "untappd", "error", err)
		return renderUntappdMock(width, height), nil
	}
	data, err := parseUntappdResponse(body)
	if err != nil {
		slog.Warn("untappd parse failed, using fallback", "source", "untappd", "error", err)
		return renderUntappdMock(width, height), nil
	}
	img, err := render.RenderDict(data, width, height, themes.UntappdTheme, "fonts/PixelifySans.ttf")
	if err != nil {
		return nil, err
	}
	slog.Info("untappd data rendered", "source", "untappd")
	return img, nil
}

func (u *UntappdDS) CurrentState(ctx context.Context) (map[string]any, error) {
	if strings.TrimSpace(u.Token) == "" && strings.TrimSpace(u.URL) == "" {
		return nil, fmt.Errorf("untappd: no credentials")
	}
	url := u.buildURL()
	body, err := apiGet(url, "", nil)
	if err != nil {
		return nil, err
	}
	data, err := parseUntappdResponse(body)
	if err != nil {
		return nil, err
	}
	m := map[string]any{}
	for k, v := range data {
		m[k] = v
	}
	return m, nil
}

func (u *UntappdDS) buildURL() string {
	token := strings.TrimSpace(u.Token)
	url := strings.TrimSpace(u.URL)
	if url != "" {
		if strings.Contains(url, "%s") {
			return fmt.Sprintf(url, token)
		}
		return url
	}
	// ponytail: single-user recent checkins only; no pagination or venue/brewery stats
	if strings.Contains(token, "/") {
		parts := strings.SplitN(token, "/", 2)
		cid := strings.TrimSpace(parts[0])
		secret := strings.TrimSpace(parts[1])
		return fmt.Sprintf("https://api.untappd.com/v4/checkin/recent?client_id=%s&client_secret=%s", cid, secret)
	}
	return fmt.Sprintf("https://api.untappd.com/v4/user/checkins?access_token=%s", token)
}

func parseUntappdResponse(body []byte) (map[string]string, error) {
	var resp struct {
		Response struct {
			Checkins struct {
				Items []struct {
					RatingScore float64 `json:"rating_score"`
					Beer        struct {
						BeerName  string  `json:"beer_name"`
						BeerAbv   float64 `json:"beer_abv"`
						BeerStyle string  `json:"beer_style"`
					} `json:"beer"`
					Brewery struct {
						BreweryName string `json:"brewery_name"`
					} `json:"brewery"`
				} `json:"items"`
				Count int `json:"count"`
			} `json:"checkins"`
			Checkin *struct {
				RatingScore float64 `json:"rating_score"`
				Beer        struct {
					BeerName  string  `json:"beer_name"`
					BeerAbv   float64 `json:"beer_abv"`
					BeerStyle string  `json:"beer_style"`
				} `json:"beer"`
				Brewery struct {
					BreweryName string `json:"brewery_name"`
				} `json:"brewery"`
			} `json:"checkin"`
			Beers *struct {
				Items []struct {
					Beer struct {
						BeerName string  `json:"beer_name"`
						BeerAbv  float64 `json:"beer_abv"`
					} `json:"beer"`
					Brewery struct {
						BreweryName string `json:"brewery_name"`
					} `json:"brewery"`
					Rating float64 `json:"rating_score"`
				} `json:"items"`
			} `json:"beers"`
		} `json:"response"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	// prefer checkins.items[0], then response.checkin, then beers.items[0]
	if len(resp.Response.Checkins.Items) > 0 {
		it := resp.Response.Checkins.Items[0]
		return map[string]string{
			"brewery": it.Brewery.BreweryName,
			"beer":    it.Beer.BeerName,
			"abv":     fmt.Sprintf("%.1f%%", it.Beer.BeerAbv),
			"rating":  fmt.Sprintf("%.1f", it.RatingScore),
		}, nil
	}
	if resp.Response.Checkin != nil && resp.Response.Checkin.Beer.BeerName != "" {
		c := resp.Response.Checkin
		return map[string]string{
			"brewery": c.Brewery.BreweryName,
			"beer":    c.Beer.BeerName,
			"abv":     fmt.Sprintf("%.1f%%", c.Beer.BeerAbv),
			"rating":  fmt.Sprintf("%.1f", c.RatingScore),
		}, nil
	}
	if resp.Response.Beers != nil && len(resp.Response.Beers.Items) > 0 {
		it := resp.Response.Beers.Items[0]
		return map[string]string{
			"brewery": it.Brewery.BreweryName,
			"beer":    it.Beer.BeerName,
			"abv":     fmt.Sprintf("%.1f%%", it.Beer.BeerAbv),
			"rating":  fmt.Sprintf("%.1f", it.Rating),
		}, nil
	}
	return nil, fmt.Errorf("untappd: no checkins in response")
}

func renderUntappdMock(width, height int) *render.RenderedImage {
	data := map[string]string{
		"brewery": "Local Brew Co. (demo)",
		"beer":    "IPA (demo)",
		"abv":     "6.5%",
		"rating":  "4.2",
	}
	img, _ := render.RenderDict(data, width, height, themes.UntappdTheme, "fonts/PixelifySans.ttf")
	return img
}
