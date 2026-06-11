package datasource

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"ledit/render"
)

type CryptoDS struct {
	Token string
	URL   string
}

func (c *CryptoDS) GetPNG() (*render.RenderedImage, error) {
	ids := "bitcoin,ethereum"
	if c.Token != "" {
		ids = c.Token
	}
	url := fmt.Sprintf("https://api.coingecko.com/api/v3/simple/price?ids=%s&vs_currencies=usd&include_24hr_change=true", ids)
	if c.URL != "" {
		url = c.URL
	}

	slog.Info("fetching crypto data", "source", "crypto", "coins", ids)
	body, err := apiGet(url, "", nil)
	if err != nil {
		slog.Warn("crypto API call failed, using fallback", "source", "crypto", "error", err)
		return fallbackCrypto(ids), nil
	}

	var resp map[string]map[string]float64
	if err := json.Unmarshal(body, &resp); err != nil || len(resp) == 0 {
		slog.Warn("crypto no data in response, using fallback", "source", "crypto", "error", err)
		return fallbackCrypto(ids), nil
	}
	slog.Info("crypto data fetched successfully", "source", "crypto", "coin_count", len(resp))

	data := map[string]string{}
	for _, id := range strings.Split(ids, ",") {
		id = strings.TrimSpace(id)
		if prices, ok := resp[id]; ok {
			usd := prices["usd"]
			change := prices["usd_24h_change"]
			label := strings.ToUpper(id[:min(4, len(id))])
			changeStr := ""
			if change != 0 {
				changeStr = fmt.Sprintf("%+.1f%%", change)
			}
			if changeStr != "" {
				data[label] = fmt.Sprintf("$%.2f %s", usd, changeStr)
			} else {
				data[label] = fmt.Sprintf("$%.2f", usd)
			}
		}
	}
	if len(data) == 0 {
		return fallbackCrypto(ids), nil
	}

	return render.RenderDict(data, 400, 400, DefaultTheme(), "fonts/PixelifySans.ttf")
}

func fallbackCrypto(ids string) *render.RenderedImage {
	data := map[string]string{}
	for _, id := range strings.Split(ids, ",") {
		id = strings.TrimSpace(id)
		if id != "" {
			data[strings.ToUpper(id[:min(4, len(id))])] = "--"
		}
	}
	img, _ := render.RenderDict(data, 400, 400, DefaultTheme(), "fonts/PixelifySans.ttf")
	return img
}
