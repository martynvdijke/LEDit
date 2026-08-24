package datasource

import (
	"strings"
	"testing"
)

func TestFormatWifiPayloadEscaping(t *testing.T) {
	cases := []struct {
		auth, ssid, pass, want string
	}{
		{"WPA", "MyWifi", "secret", "WIFI:T:WPA;S:MyWifi;P:secret;;"},
		{"WPA", "A;B", "p:1", "WIFI:T:WPA;S:A\\;B;P:p\\:1;;"},
		{"WPA", `a\b`, `c"d`, `WIFI:T:WPA;S:a\\b;P:c\"d;;`},
		{"WPA", "a,b", "x,y", "WIFI:T:WPA;S:a\\,b;P:x\\,y;;"},
		{"WPA", "s:;\\,\"", "p:;\\,\"", "WIFI:T:WPA;S:s\\:\\;\\\\\\,\\\";P:p\\:\\;\\\\\\,\\\";;"},
		{"", "MyWifi", "secret", "WIFI:T:WPA;S:MyWifi;P:secret;;"},
	}
	for _, tc := range cases {
		got := FormatWifiPayload(tc.auth, tc.ssid, tc.pass)
		if got != tc.want {
			t.Errorf("FormatWifiPayload(%q,%q,%q)=%q want %q", tc.auth, tc.ssid, tc.pass, got, tc.want)
		}
	}
}

func TestQRSourceValidateLongContentECC(t *testing.T) {
	// Content within 512 runes but large byte length (multi-byte) should fail for H but succeed for M.
	long := strings.Repeat("€", 500) // 1500 bytes
	if len([]rune(long)) > 512 {
		t.Fatalf("test content rune len %d exceeds 512", len([]rune(long)))
	}
	qH := &QRSource{Content: long, Mode: "text", ErrorCorrection: "H", QuietZone: 4, WifiAuth: "WPA"}
	if err := qH.Validate(); err == nil {
		t.Fatalf("expected H to fail for long content, got nil")
	} else if !strings.Contains(err.Error(), "too long for ECC") {
		t.Fatalf("expected ECC error, got %v", err)
	}
	qM := &QRSource{Content: long, Mode: "text", ErrorCorrection: "M", QuietZone: 4, WifiAuth: "WPA"}
	if err := qM.Validate(); err != nil {
		t.Fatalf("M should succeed for same content, got %v", err)
	}
}

func TestQRSourceValidateWifiFields(t *testing.T) {
	if err := (&QRSource{Content: "x", Mode: "wifi", WifiSSID: "", WifiAuth: "WPA", ErrorCorrection: "M", QuietZone: 4}).Validate(); err == nil {
		t.Error("wifi without ssid should fail")
	}
	if err := (&QRSource{Content: "x", Mode: "wifi", WifiSSID: "s", WifiAuth: "WPA", WifiPassword: "", ErrorCorrection: "M", QuietZone: 4}).Validate(); err == nil {
		t.Error("WPA without password should fail")
	}
	// nopass without password should pass
	if err := (&QRSource{Content: "x", Mode: "wifi", WifiSSID: "s", WifiAuth: "nopass", ErrorCorrection: "M", QuietZone: 4}).Validate(); err != nil {
		t.Errorf("nopass without password should pass, got %v", err)
	}
}

func TestQRSourcePayload(t *testing.T) {
	q := &QRSource{Content: "hello", Mode: "text"}
	if q.Payload() != "hello" {
		t.Fatalf("payload text got %q", q.Payload())
	}
	q2 := &QRSource{Mode: "wifi", WifiSSID: "MyWifi", WifiPassword: "pw", WifiAuth: "WPA"}
	if got := q2.Payload(); got != "WIFI:T:WPA;S:MyWifi;P:pw;;" {
		t.Fatalf("wifi payload got %q", got)
	}
}
