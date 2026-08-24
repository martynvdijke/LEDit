package datasource

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"ledit/render"
)

type GitHubDS struct {
	Token string
	URL   string
}

func (g *GitHubDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	parts := strings.SplitN(g.Token, "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		slog.Warn("github malformed token, using fallback", "source", "github", "token", g.Token)
		return fallbackGitHub(width, height), nil
	}

	url := g.URL
	if url == "" {
		url = "https://api.github.com/repos/%s"
	}
	if strings.Contains(url, "%s") {
		url = fmt.Sprintf(url, g.Token)
	}

	slog.Info("fetching github data", "source", "github", "repo", g.Token)
	body, err := apiGet(url, "", nil)
	if err != nil {
		slog.Warn("github API call failed, using fallback", "source", "github", "error", err)
		return fallbackGitHub(width, height), nil
	}

	rows, err := BuildGitHubRows(body, time.Now())
	if err != nil {
		slog.Warn("github parse failed, using fallback", "source", "github", "error", err)
		return fallbackGitHub(width, height), nil
	}

	data := map[string]string{
		"title": "GITHUB",
	}
	for _, r := range rows {
		data[r[0]] = r[1]
	}

	img, err := render.RenderDict(data, width, height, DefaultTheme(), "fonts/PixelifySans.ttf")
	if err != nil {
		return nil, err
	}
	slog.Info("github data rendered", "source", "github", "repo", g.Token)
	return img, nil
}

func fallbackGitHub(width, height int) *render.RenderedImage {
	data := map[string]string{
		"title": "GITHUB",
		"r1":    "unavailable",
	}
	img, _ := render.RenderDict(data, width, height, DefaultTheme(), "fonts/PixelifySans.ttf")
	return img
}

// BuildGitHubRows parses repo JSON and returns rows for STARS, ISSUES, FORKS, PUSH.
func BuildGitHubRows(body []byte, now time.Time) ([][2]string, error) {
	var resp struct {
		Stargazers int    `json:"stargazers_count"`
		Issues     int    `json:"open_issues_count"`
		Forks      int    `json:"forks_count"`
		PushedAt   string `json:"pushed_at"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}

	relative := "unknown"
	if resp.PushedAt != "" {
		if t, err := parseGitHubTime(resp.PushedAt); err == nil {
			diff := now.Sub(t)
			if diff < 0 {
				diff = 0
			}
			switch {
			case diff < time.Hour:
				relative = fmt.Sprintf("%dm", int(diff.Minutes()))
			case diff < 24*time.Hour:
				relative = fmt.Sprintf("%dh", int(diff.Hours()))
			default:
				relative = fmt.Sprintf("%dd", int(diff.Hours()/24))
			}
		}
	}

	rows := [][2]string{
		{"STARS", fmt.Sprintf("%d", resp.Stargazers)},
		{"ISSUES", fmt.Sprintf("%d", resp.Issues)},
		{"FORKS", fmt.Sprintf("%d", resp.Forks)},
		{"PUSH", relative},
	}
	return rows, nil
}

func parseGitHubTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}
