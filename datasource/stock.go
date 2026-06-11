package datasource

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"ledit/render"
)

type StockDS struct {
	Token string
	URL   string
}

func (s *StockDS) GetPNG() (*render.RenderedImage, error) {
	symbols := "AAPL,MSFT,GOOGL"
	if s.Token != "" {
		symbols = s.Token
	}

	slog.Info("fetching stock data", "source", "stock", "symbols", symbols)
	data := map[string]string{}
	for _, sym := range strings.Split(symbols, ",") {
		sym = strings.TrimSpace(sym)
		if sym == "" {
			continue
		}
		price, change := fetchStockPrice(sym)
		label := strings.ToUpper(sym[:min(5, len(sym))])
		changeStr := ""
		if change != "" {
			changeStr = change
		}
		if price != "" {
			if changeStr != "" {
				data[label] = fmt.Sprintf("$%s %s", price, changeStr)
			} else {
				data[label] = fmt.Sprintf("$%s", price)
			}
		}
	}

	if len(data) == 0 {
		slog.Warn("stock all symbols failed, using fallback", "source", "stock")
		for _, sym := range strings.Split(symbols, ",") {
			sym = strings.TrimSpace(sym)
			if sym != "" {
				label := strings.ToUpper(sym[:min(5, len(sym))])
				data[label] = "--"
			}
		}
	} else {
		slog.Info("stock data fetched successfully", "source", "stock", "symbols_found", len(data))
	}

	return render.RenderDict(data, 400, 400, DefaultTheme(), "fonts/PixelifySans.ttf")
}

func fetchStockPrice(symbol string) (price, change string) {
	url := fmt.Sprintf("https://query1.finance.yahoo.com/v8/finance/chart/%s?interval=1d&range=5d", symbol)
	body, err := apiGet(url, "", map[string]string{
		"User-Agent": "Mozilla/5.0",
	})
	if err != nil {
		slog.Warn("stock price fetch failed", "source", "stock", "symbol", symbol, "error", err)
		return "", ""
	}

	bodyStr := string(body)
	priceRaw := extractJSONFloat(bodyStr, "regularMarketPrice")
	prevClose := extractJSONFloat(bodyStr, "regularMarketPreviousClose")

	if priceRaw == "" {
		return "", ""
	}

	p, err := strconv.ParseFloat(priceRaw, 64)
	if err != nil {
		return "", ""
	}

	priceStr := fmt.Sprintf("%.2f", p)

	if prevClose != "" {
		pc, err := strconv.ParseFloat(prevClose, 64)
		if err == nil && pc > 0 {
			diff := p - pc
			pct := (diff / pc) * 100
			changeStr := fmt.Sprintf("%+.2f (%+.2f%%)", diff, pct)
			return priceStr, changeStr
		}
	}

	return priceStr, ""
}

func extractJSONFloat(body, key string) string {
	patterns := []string{
		"\"" + key + "\":{\"raw\":",
	}
	for _, pat := range patterns {
		idx := strings.Index(body, pat)
		if idx >= 0 {
			start := idx + len(pat)
			end := strings.IndexByte(body[start:], ',')
			if end < 0 {
				end = strings.IndexByte(body[start:], '}')
			}
			if end > 0 {
				val := strings.TrimSpace(body[start : start+end])
				return val
			}
		}
	}
	return ""
}
