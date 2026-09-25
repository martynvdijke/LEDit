package datasource

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

func apiGet(url, token string, headers map[string]string) ([]byte, error) {
	return apiRequest(context.Background(), http.MethodGet, url, token, headers, nil)
}

// apiRequest performs an outbound call with an optional JSON body. Token is sent
// as X-API-Key unless an explicit Authorization header is supplied in headers.
func apiRequest(ctx context.Context, method, url, token string, headers map[string]string, body []byte) ([]byte, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	hasAuth := false
	for k := range headers {
		if strings.EqualFold(k, "Authorization") {
			hasAuth = true
			break
		}
	}
	if token != "" && !hasAuth {
		req.Header.Set("X-API-Key", token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	return out, nil
}

func (s *SonarrDS) apiGet(path string) ([]byte, error) {
	url := s.URL + path
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Api-Key", s.Token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
