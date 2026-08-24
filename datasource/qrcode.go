package datasource

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/skip2/go-qrcode"
	"ledit/render"
)

// QRSource holds QR datasource config.
type QRSource struct {
	Content         string
	Mode            string // text|url|wifi
	WifiSSID        string
	WifiPassword    string
	WifiAuth        string // WPA|WEP|nopass
	Caption         string
	ErrorCorrection string // L|M|Q|H
	QuietZone       int
}

// Payload returns the string to encode.
func (q *QRSource) Payload() string {
	if q.Mode == "wifi" {
		return FormatWifiPayload(q.WifiAuth, q.WifiSSID, q.WifiPassword)
	}
	return q.Content
}

// escapeWifi escapes \ ; : , " in wifi fields per spec.
func escapeWifi(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, ";", "\\;")
	s = strings.ReplaceAll(s, ",", "\\,")
	s = strings.ReplaceAll(s, ":", "\\:")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}

// FormatWifiPayload builds WIFI:T:...;S:...;P:...;; string.
func FormatWifiPayload(auth, ssid, password string) string {
	if auth == "" {
		auth = "WPA"
	}
	return fmt.Sprintf("WIFI:T:%s;S:%s;P:%s;;", escapeWifi(auth), escapeWifi(ssid), escapeWifi(password))
}

// Validate checks field constraints and trial QR generation.
func (q *QRSource) Validate() error {
	if utf8.RuneCountInString(q.Content) == 0 || utf8.RuneCountInString(q.Content) > 512 {
		return fmt.Errorf("content must be 1-512 chars")
	}
	if q.Mode != "text" && q.Mode != "url" && q.Mode != "wifi" {
		return fmt.Errorf("mode must be text, url or wifi")
	}
	if q.Mode == "wifi" {
		if len(q.WifiSSID) == 0 || len(q.WifiSSID) > 32 {
			return fmt.Errorf("wifi_ssid must be 1-32 chars when mode is wifi")
		}
		if q.WifiAuth != "WPA" && q.WifiAuth != "WEP" && q.WifiAuth != "nopass" {
			return fmt.Errorf("wifi_auth must be WPA, WEP or nopass")
		}
		if q.WifiAuth == "WPA" && q.WifiPassword == "" {
			return fmt.Errorf("wifi password required for WPA")
		}
	}
	if utf8.RuneCountInString(q.Caption) > 64 {
		return fmt.Errorf("caption must be 0-64 chars")
	}
	if q.ErrorCorrection != "L" && q.ErrorCorrection != "M" && q.ErrorCorrection != "Q" && q.ErrorCorrection != "H" {
		return fmt.Errorf("error_correction must be L, M, Q or H")
	}
	if q.QuietZone < 0 || q.QuietZone > 8 {
		return fmt.Errorf("quiet_zone must be 0-8")
	}
	// Trial generation
	level := eccLevel(q.ErrorCorrection)
	payload := q.Payload()
	if _, err := qrcode.New(payload, level); err != nil {
		return fmt.Errorf("content too long for ECC %s — try lower ECC or shorter content: %w", q.ErrorCorrection, err)
	}
	return nil
}

func eccLevel(s string) qrcode.RecoveryLevel {
	switch s {
	case "L":
		return qrcode.Low
	case "M":
		return qrcode.Medium
	case "Q":
		return qrcode.High
	case "H":
		return qrcode.Highest
	default:
		return qrcode.Medium
	}
}

// GetPNG renders via render package.
func (q *QRSource) GetPNG(width, height int) (*render.RenderedImage, error) {
	return render.RenderQRCode(render.QRCodeParams{
		Payload:         q.Payload(),
		Caption:         q.Caption,
		ErrorCorrection: q.ErrorCorrection,
		QuietZone:       q.QuietZone,
		Width:           width,
		Height:          height,
	})
}
