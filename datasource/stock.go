package datasource

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"ledit/render"
)

var _ StateProvider = (*StockDS)(nil)

var stockAPIBaseURL = "https://query1.finance.yahoo.com/v8/finance/chart/%s?interval=1d&range=5d"

type StockDS struct {
	Token string
	URL   string
}

func (s *StockDS) stockURL(symbol string) string {
	if s.URL != "" {
		if strings.Contains(s.URL, "%s") {
			return fmt.Sprintf(s.URL, symbol)
		}
		return s.URL
	}
	return fmt.Sprintf(stockAPIBaseURL, symbol)
}

func (s *StockDS) fetchStockPriceWithURL(symbol string) (price, change string) {
	url := s.stockURL(symbol)
	body, err := apiGet(url, "", map[string]string{"User-Agent": "Mozilla/5.0"})
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

func (s *StockDS) CurrentState(ctx context.Context) (map[string]any, error) {
	symbols := "AAPL"
	if s.Token != "" {
		for sym := range strings.SplitSeq(s.Token, ",") {
			sym = strings.TrimSpace(sym)
			if sym != "" {
				symbols = sym
				break
			}
		}
	}
	priceStr, changeStr := s.fetchStockPriceWithURL(symbols)
	if priceStr == "" {
		return nil, fmt.Errorf("stock: no price for %s", symbols)
	}
	price, err := strconv.ParseFloat(priceStr, 64)
	if err != nil {
		return nil, err
	}
	var change float64
	if changeStr != "" {
		// changeStr is like "+5.00 (+2.56%)" -> take first number
		first := strings.Fields(changeStr)[0]
		if v, err := strconv.ParseFloat(first, 64); err == nil {
			change = v
		}
	}
	return map[string]any{"price": price, "change": change}, nil
}

func (s *StockDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	symbols := "AAPL,MSFT,GOOGL"
	if s.Token != "" {
		symbols = s.Token
	}

	slog.Info("fetching stock data", "source", "stock", "symbols", symbols)
	data := map[string]string{}
	for sym := range strings.SplitSeq(symbols, ",") {
		sym = strings.TrimSpace(sym)
		if sym == "" {
			continue
		}
		price, change := s.fetchStockPriceWithURL(sym)
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
		for sym := range strings.SplitSeq(symbols, ",") {
			sym = strings.TrimSpace(sym)
			if sym != "" {
				label := strings.ToUpper(sym[:min(5, len(sym))])
				data[label] = "--"
			}
		}
	} else {
		slog.Info("stock data fetched successfully", "source", "stock", "symbols_found", len(data))
		for _, v := range data {
			var f float64
			s := strings.TrimPrefix(v, "$")
			s = strings.Fields(s)[0]
			if _, err := fmt.Sscanf(s, "%f", &f); err == nil {
				RecordChartValue(f)
				break
			}
		}
	}

	return render.RenderDict(data, width, height, DefaultTheme(), "fonts/PixelifySans.ttf")
}

func fetchStockPrice(symbol string) (price, change string) {
	return (&StockDS{}).fetchStockPriceWithURL(symbol)
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
				val = strings.Trim(val, "}")
				return val
			}
		}
	}
	return ""
}
