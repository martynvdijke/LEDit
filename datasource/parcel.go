package datasource

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"ledit/render"
)

var _ StateProvider = (*ParcelDS)(nil)

// ParcelDS fetches parcel tracking.
// ponytail: first parcel only.
type ParcelDS struct {
	Token string
	URL   string
}

// BuildParcelRows handles multiple shapes.
func BuildParcelRows(body []byte) ([][2]string, error) {
	// shape 1: {"data":[{"tracking_number":...,"status":...,"substatus":...}]}
	var s1 struct {
		Data []struct {
			TrackingNumber string `json:"tracking_number"`
			Status         string `json:"status"`
			SubStatus      string `json:"substatus"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &s1); err == nil && s1.Data != nil {
		if len(s1.Data) > 0 {
			st := s1.Data[0].Status
			if s1.Data[0].SubStatus != "" {
				st = s1.Data[0].SubStatus
			}
			rows := [][2]string{
				{"TRACKING", s1.Data[0].TrackingNumber},
				{"STATUS", strings.ToUpper(st)},
			}
			for i := range rows {
				if len(rows[i][1]) > 28 {
					rows[i][1] = rows[i][1][:28]
				}
			}
			return rows, nil
		}
		return nil, fmt.Errorf("parcel: empty data array")
	}
	// shape 2: {"data":{"items":[{"tracking_number":...,"status":...}]}}
	var s2 struct {
		Data struct {
			Items []struct {
				TrackingNumber string `json:"tracking_number"`
				Status         string `json:"status"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &s2); err == nil && s2.Data.Items != nil {
		if len(s2.Data.Items) > 0 {
			rows := [][2]string{
				{"TRACKING", s2.Data.Items[0].TrackingNumber},
				{"STATUS", strings.ToUpper(s2.Data.Items[0].Status)},
			}
			for i := range rows {
				if len(rows[i][1]) > 28 {
					rows[i][1] = rows[i][1][:28]
				}
			}
			return rows, nil
		}
	}
	// shape 3: {"status":...,"tracking_number":...}
	var s3 struct {
		Status         string `json:"status"`
		TrackingNumber string `json:"tracking_number"`
		SubStatus      string `json:"substatus"`
	}
	if err := json.Unmarshal(body, &s3); err == nil && s3.Status != "" {
		st := s3.Status
		if s3.SubStatus != "" {
			st = s3.SubStatus
		}
		rows := [][2]string{
			{"TRACKING", s3.TrackingNumber},
			{"STATUS", strings.ToUpper(st)},
		}
		for i := range rows {
			if len(rows[i][1]) > 28 {
				rows[i][1] = rows[i][1][:28]
			}
		}
		return rows, nil
	}
	return nil, fmt.Errorf("parcel: unrecognized response")
}

func fallbackParcel(width, height int) *render.RenderedImage {
	theme := DefaultTheme()
	theme.Title = "PARCEL"
	data := map[string]string{"PARCEL": "unavailable"}
	img, _ := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
	return img
}

func (p *ParcelDS) GetPNG(width, height int) (*render.RenderedImage, error) {
	base := p.URL
	if strings.TrimSpace(base) == "" {
		base = "https://api.17track.net/track/v2/gettrackinfo"
	}
	slog.Info("fetching parcel data", "source", "parcel")
	body, err := apiGet(base, p.Token, nil)
	if err != nil {
		slog.Warn("parcel API call failed, using fallback", "source", "parcel", "error", err)
		return fallbackParcel(width, height), nil
	}
	rows, err := BuildParcelRows(body)
	if err != nil {
		slog.Warn("parcel parse failed, using fallback", "source", "parcel", "error", err)
		return fallbackParcel(width, height), nil
	}
	data := make(map[string]string, len(rows))
	for _, r := range rows {
		data[r[0]] = r[1]
	}
	theme := DefaultTheme()
	theme.Title = "PARCEL"
	img, err := render.RenderDict(data, width, height, theme, "fonts/PixelifySans.ttf")
	if err != nil {
		return nil, err
	}
	return img, nil
}

func (p *ParcelDS) CurrentState(_ context.Context) (map[string]any, error) {
	base := p.URL
	if strings.TrimSpace(base) == "" {
		base = "https://api.17track.net/track/v2/gettrackinfo"
	}
	body, err := apiGet(base, p.Token, nil)
	if err != nil {
		return nil, err
	}
	rows, err := BuildParcelRows(body)
	if err != nil {
		return nil, err
	}
	m := make(map[string]any, len(rows))
	for _, r := range rows {
		switch r[0] {
		case "TRACKING":
			m["tracking_number"] = r[1]
		case "STATUS":
			m["status"] = r[1]
		}
	}
	return m, nil
}
