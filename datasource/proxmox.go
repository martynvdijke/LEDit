package datasource

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"ledit/render"
)

var _ StateProvider = (*ProxmoxDS)(nil)

// ProxmoxDS fetches Proxmox VE node status.
// ponytail: nodes only, no QEMU/LXC detail.
type ProxmoxDS struct {
	Token string
	URL   string
}

// BuildProxmoxRows parses {"data":[{"node":string,"status":string,"cpu":float,"maxmem":int,"mem":int}]}
func BuildProxmoxRows(body []byte) ([][2]string, error) {
	var resp struct {
		Data []struct {
			Node   string  `json:"node"`
			Status string  `json:"status"`
			CPU    float64 `json:"cpu"`
			MaxMem int64   `json:"maxmem"`
			Mem    int64   `json:"mem"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("proxmox parse error: %w", err)
	}
	if resp.Data == nil {
		return nil, fmt.Errorf("proxmox: missing data field")
	}
	rows := [][2]string{
		{"NODES", fmt.Sprintf("%d", len(resp.Data))},
	}
	if len(resp.Data) > 0 {
		rows = append(rows, [2]string{"CPU%", fmt.Sprintf("%.0f%%", resp.Data[0].CPU*100)})
	}
	for i := range rows {
		if len(rows[i][1]) > 28 {
			rows[i][1] = rows[i][1][:28]
		}
	}
	return rows, nil
}

func fallbackProxmox(width, height int) *render.RenderedImage {
	theme := DefaultTheme()
	theme.Title = "PROXMOX"
	data := map[string]string{"PROXMOX": "unavailable"}
	img, _ := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
	return img
}

func (p *ProxmoxDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	base := p.URL
	if strings.TrimSpace(base) == "" {
		base = "https://proxmox.example.com:8006/api2/json/nodes"
	}
	headers := map[string]string{}
	if p.Token != "" {
		headers["Authorization"] = "PVEAPIToken=" + p.Token
	}
	slog.Info("fetching proxmox data", "source", "proxmox")
	body, err := apiGet(base, "", headers)
	if err != nil {
		slog.Warn("proxmox API call failed, using fallback", "source", "proxmox", "error", err)
		return fallbackProxmox(width, height), nil
	}
	rows, err := BuildProxmoxRows(body)
	if err != nil {
		slog.Warn("proxmox parse failed, using fallback", "source", "proxmox", "error", err)
		return fallbackProxmox(width, height), nil
	}
	data := make(map[string]string, len(rows))
	for _, r := range rows {
		data[r[0]] = r[1]
	}
	theme := DefaultTheme()
	theme.Title = "PROXMOX"
	img, err := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
	if err != nil {
		return nil, err
	}
	return img, nil
}

func (p *ProxmoxDS) CurrentState(_ context.Context) (map[string]any, error) {
	base := p.URL
	if strings.TrimSpace(base) == "" {
		base = "https://proxmox.example.com:8006/api2/json/nodes"
	}
	headers := map[string]string{}
	if p.Token != "" {
		headers["Authorization"] = "PVEAPIToken=" + p.Token
	}
	body, err := apiGet(base, "", headers)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data []struct {
			Node string  `json:"node"`
			CPU  float64 `json:"cpu"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	if resp.Data == nil {
		return nil, fmt.Errorf("proxmox: missing data field")
	}
	cpu := 0.0
	node := ""
	if len(resp.Data) > 0 {
		cpu = resp.Data[0].CPU * 100
		node = resp.Data[0].Node
	}
	return map[string]any{
		"nodes": len(resp.Data),
		"cpu":   cpu,
		"node":  node,
	}, nil
}
