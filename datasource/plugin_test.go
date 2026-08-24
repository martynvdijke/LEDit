package datasource

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func makePNG(w, h int) string {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestValidateRowsCaps(t *testing.T) {
	rows := make([]PluginRow, 30)
	if err := ValidateRows(rows); err == nil {
		t.Fatal("expected error for 30 rows")
	}
	if err := ValidateRows([]PluginRow{{Label: strings.Repeat("a", 65)}}); err == nil {
		t.Fatal("expected label too long")
	}
	if err := ValidateRows([]PluginRow{{Value: strings.Repeat("a", 33)}}); err == nil {
		t.Fatal("expected value too long")
	}
	if err := ValidateRows([]PluginRow{{Text: strings.Repeat("a", 257)}}); err == nil {
		t.Fatal("expected text too long")
	}
}

func TestValidatePNG(t *testing.T) {
	b64 := makePNG(64, 32)
	if _, err := ValidatePNG(b64, 64, 32); err != nil {
		t.Fatalf("valid png: %v", err)
	}
	if _, err := ValidatePNG(b64, 10, 10); err == nil {
		t.Fatal("expected dimension mismatch")
	}
	if _, err := ValidatePNG("notbase64!!!", 64, 32); err == nil {
		t.Fatal("expected b64 error")
	}
}

func TestParsePluginResponseWrongV(t *testing.T) {
	data := []byte(`{"v":2,"rows":[]}`)
	if _, err := ParsePluginResponse(data, 64, 32); err == nil {
		t.Fatal("expected wrong v")
	}
	data2 := []byte(`{"v":1,"unknown":1}`)
	if _, err := ParsePluginResponse(data2, 64, 32); err == nil {
		t.Fatal("expected unknown field or missing variant")
	}
}

func TestParseRowsValidation(t *testing.T) {
	rows := strings.Repeat(`{"label":"a","value":"b","text":"c"},`, 21)
	rows = "[" + strings.TrimSuffix(rows, ",") + "]"
	data := []byte(`{"v":1,"rows":` + rows + `}`)
	if _, err := ParsePluginResponse(data, 64, 32); err == nil {
		t.Fatal("expected too many rows")
	}
}

func TestRequestSerialization(t *testing.T) {
	req := PluginRequest{V: 1, Config: json.RawMessage(`{"x":1}`), Width: 64, Height: 32, Timestamp: time.Now().Format(time.RFC3339), DeviceID: 5}
	b, _ := json.Marshal(req)
	var out PluginRequest
	_ = json.Unmarshal(b, &out)
	if out.V != 1 || out.Width != 64 {
		t.Fatal("serialization mismatch")
	}
}

func TestIsLocalhostHost(t *testing.T) {
	if !IsLocalhostHost("localhost") || !IsLocalhostHost("127.0.0.1") || !IsLocalhostHost("::1") {
		t.Fatal("localhost check failed")
	}
	if IsLocalhostHost("example.com") {
		t.Fatal("should reject example.com")
	}
}

func TestExecTransportSuccess(t *testing.T) {
	ctx := context.Background()
	body := []byte(`{"v":1,"rows":[{"label":"Temp","value":"22","text":"ok"}]}`)
	tr := execTransport(ctx, "/bin/cat", body)
	if tr.err != nil {
		t.Fatalf("exec err: %v", tr.err)
	}
	if _, err := ParsePluginResponse(tr.stdout, 64, 32); err != nil {
		t.Fatalf("parse: %v", err)
	}
}

func TestExecTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	tr := execTransport(ctx, "/bin/sleep", []byte(`{}`))
	// /bin/sleep without args exits quickly; we test timeout via InvokePlugin with short timeout instead
	_ = tr
	// Test InvokePlugin timeout with http server that sleeps
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.Write([]byte(`{"v":1,"rows":[]}`))
	}))
	defer srv.Close()
	plugin := PluginInfo{ID: 99, Kind: "http", Target: srv.URL, TimeoutMs: 50}
	req := PluginRequest{Width: 64, Height: 32, Config: json.RawMessage(`{}`)}
	_, tr2 := InvokePlugin(context.Background(), plugin, req)
	if tr2.err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestHTTPTransport(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content-type")
		}
		w.Write([]byte(`{"v":1,"rows":[{"label":"a","value":"1","text":"t"}]}`))
	}))
	defer srv.Close()
	// httptest host is 127.0.0.1, should pass localhost check via IsLocalhostHost on 127.0.0.1
	tr := httpTransport(context.Background(), srv.URL, []byte(`{"v":1}`))
	if tr.err != nil {
		t.Fatalf("http err: %v", tr.err)
	}
	if _, err := ParsePluginResponse(tr.stdout, 64, 32); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPNonLocalhostRejected(t *testing.T) {
	tr := httpTransport(context.Background(), "http://example.com/api", []byte(`{}`))
	if tr.err == nil {
		t.Fatal("expected rejection")
	}
}

func TestInvokePluginDispatcher(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"v":1,"rows":[{"label":"x","value":"y","text":"z"}]}`))
	}))
	defer srv.Close()
	plugin := PluginInfo{ID: 1, Kind: "http", Target: srv.URL, TimeoutMs: 1000}
	req := PluginRequest{Width: 64, Height: 32, Config: json.RawMessage(`{}`)}
	resp, tr := InvokePlugin(context.Background(), plugin, req)
	if tr.err != nil || resp == nil {
		t.Fatalf("invoke failed: %v %v", tr.err, resp)
	}
}
