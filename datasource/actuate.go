package datasource

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// haSegmentRe constrains HA service-path segments (no traversal / scheme injection).
var haSegmentRe = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

var (
	_ Actuator = (*HomeAssistantDS)(nil)
	_ Actuator = (*QBittorrentDS)(nil)
	_ Actuator = (*OverseerrDS)(nil)
	_ Actuator = (*GenericAPIDS)(nil)
)

// HomeAssistantDS.Actuate — only ActionCallService
func (h *HomeAssistantDS) Actuate(ctx context.Context, action string, params map[string]string) error {
	if action != ActionCallService {
		return fmt.Errorf("unsupported action %q for homeassistant", action)
	}
	domain := params["domain"]
	service := params["service"]
	entityID := params["entity_id"]
	if domain == "" || service == "" || entityID == "" {
		return fmt.Errorf("homeassistant call_service requires domain, service, entity_id")
	}
	if !haSegmentRe.MatchString(domain) || !haSegmentRe.MatchString(service) {
		return fmt.Errorf("invalid domain/service %q/%q", domain, service)
	}
	bodyMap := map[string]any{"entity_id": entityID}
	if dataStr := params["data"]; dataStr != "" {
		var extra map[string]any
		if err := json.Unmarshal([]byte(dataStr), &extra); err != nil {
			return fmt.Errorf("invalid data JSON: %w", err)
		}
		for k, v := range extra {
			if k == "entity_id" {
				continue
			}
			bodyMap[k] = v
		}
	}
	body, err := json.Marshal(bodyMap)
	if err != nil {
		return fmt.Errorf("failed to marshal body: %w", err)
	}
	base := strings.TrimRight(h.URL, "/")
	u := base + "/api/services/" + domain + "/" + service
	headers := map[string]string{"Authorization": "Bearer " + h.Token}
	_, err = apiRequest(ctx, http.MethodPost, u, "", headers, body)
	if err != nil {
		return fmt.Errorf("homeassistant actuate failed: %w", err)
	}
	return nil
}

func qbLoginClient(ctx context.Context, base, userpass string) (*http.Client, []*http.Cookie, error) {
	if !strings.Contains(userpass, ":") {
		return nil, nil, fmt.Errorf("malformed token")
	}
	parts := strings.SplitN(userpass, ":", 2)
	username := parts[0]
	password := ""
	if len(parts) == 2 {
		password = parts[1]
	}
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Timeout: 10 * time.Second, Jar: jar}
	form := url.Values{}
	form.Set("username", username)
	form.Set("password", password)
	loginURL := base + "/api/v2/auth/login"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loginURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("login status %d body %s", resp.StatusCode, string(body))
	}
	hasSID := false
	u, _ := url.Parse(loginURL)
	for _, c := range jar.Cookies(u) {
		if c.Name == "SID" && c.Value != "" {
			hasSID = true
			break
		}
	}
	if !hasSID {
		for _, c := range resp.Cookies() {
			if c.Name == "SID" && c.Value != "" {
				hasSID = true
				break
			}
		}
	}
	if !hasSID {
		return nil, nil, fmt.Errorf("missing SID cookie body %s", string(body))
	}
	return client, jar.Cookies(u), nil
}

func (q *QBittorrentDS) Actuate(ctx context.Context, action string, params map[string]string) error {
	if action != ActionPause && action != ActionResume {
		return fmt.Errorf("unsupported action %q for qbittorrent", action)
	}
	if q.URL == "" || q.Token == "" {
		return fmt.Errorf("unconfigured")
	}
	base := strings.TrimRight(q.URL, "/")
	client, _, err := qbLoginClient(ctx, base, q.Token)
	if err != nil {
		return fmt.Errorf("qbittorrent login failed: %w", err)
	}
	hashes := params["hashes"]
	if hashes == "" {
		hashes = "all"
	}
	var endpoint string
	if action == ActionPause {
		endpoint = base + "/api/v2/torrents/pause"
	} else {
		endpoint = base + "/api/v2/torrents/resume"
	}
	form := url.Values{}
	form.Set("hashes", hashes)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("qbittorrent actuate failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API returned status %d body %s", resp.StatusCode, string(b))
	}
	return nil
}

func (o *OverseerrDS) Actuate(ctx context.Context, action string, params map[string]string) error {
	if action != ActionApprove {
		return fmt.Errorf("unsupported action %q for overseerr", action)
	}
	idStr := params["request_id"]
	if idStr == "" {
		return fmt.Errorf("overseerr approve requires request_id")
	}
	if _, err := strconv.Atoi(idStr); err != nil {
		return fmt.Errorf("invalid request_id %q: %w", idStr, err)
	}
	base := strings.TrimRight(o.URL, "/")
	u := base + "/api/v1/request/" + idStr + "/approve"
	_, err := apiRequest(ctx, http.MethodPost, u, o.Token, nil, nil)
	if err != nil {
		return fmt.Errorf("overseerr actuate failed: %w", err)
	}
	return nil
}

func (g *GenericAPIDS) Actuate(ctx context.Context, action string, params map[string]string) error {
	if action != ActionRequest {
		return fmt.Errorf("unsupported action %q for genericapi", action)
	}
	method := params["method"]
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return fmt.Errorf("invalid method %q", method)
	}
	rawPath := params["path"]
	if rawPath == "" {
		return fmt.Errorf("path is required")
	}
	decoded, err := url.PathUnescape(rawPath)
	if err != nil {
		return fmt.Errorf("invalid path %q", rawPath)
	}
	if strings.Contains(decoded, "\\") || strings.Contains(decoded, "://") || strings.HasPrefix(decoded, "//") || strings.Contains(decoded, "..") {
		return fmt.Errorf("invalid path %q", rawPath)
	}
	cleanPath := path.Clean("/" + decoded)
	base := strings.TrimRight(g.URL, "/")
	u := base + cleanPath
	var body []byte
	if b := params["body"]; b != "" {
		body = []byte(b)
	}
	cfg := ParseGenericAPIConfig(g.Config)
	headers := cfg.Headers
	_, err = apiRequest(ctx, method, u, g.Token, headers, body)
	if err != nil {
		return fmt.Errorf("genericapi actuate failed: %w", err)
	}
	return nil
}
