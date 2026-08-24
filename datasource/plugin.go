package datasource

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"

	"ledit/render"
)

// Contract constants.
const (
	PluginVersion    = 1
	MaxLabelLen      = 64
	MaxValueLen      = 32
	MaxTextLen       = 256
	MaxRows          = 20
	MaxPNGB64Bytes   = 512 * 1024
	StderrCap        = 4096
	DefaultPluginTTL = 0
)

// PluginRequest is sent to plugins.
type PluginRequest struct {
	V         int             `json:"v"`
	Config    json.RawMessage `json:"config"`
	Width     int             `json:"width"`
	Height    int             `json:"height"`
	Timestamp string          `json:"timestamp"`
	DeviceID  int             `json:"device_id"`
}

// PluginRow is one row in rows variant.
type PluginRow struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Text  string `json:"text"`
	Icon  string `json:"icon,omitempty"`
}

// PluginRowsResponse variant.
type PluginRowsResponse struct {
	V          int         `json:"v"`
	Rows       []PluginRow `json:"rows"`
	TTLSeconds *int        `json:"ttl_seconds,omitempty"`
}

// PluginPNGResponse variant.
type PluginPNGResponse struct {
	V          int    `json:"v"`
	PNGB64     string `json:"png_b64"`
	TTLSeconds *int   `json:"ttl_seconds,omitempty"`
}

// PluginResponse is unified return from invokePlugin.
type PluginResponse struct {
	Rows   []PluginRow
	PNGB64 string
	TTL    int
	IsPNG  bool
}

// PluginInfo describes a registered plugin for invocation.
type PluginInfo struct {
	ID        int
	Name      string
	Kind      string // exec|http
	Target    string
	Enabled   bool
	TimeoutMs int
}

// pluginHealthEntry tracks last invocation.
type pluginHealthEntry struct {
	Enabled       bool       `json:"enabled"`
	LastLatencyMs int64      `json:"last_latency_ms"`
	LastExitCode  *int       `json:"last_exit_code"`
	LastError     string     `json:"last_error"`
	StderrTail    string     `json:"stderr_tail"`
	LastInvokedAt *time.Time `json:"last_invoked_at"`
}

var (
	pluginHealthMu sync.RWMutex
	pluginHealth   = map[int]*pluginHealthEntry{}
)

// RecordPluginHealth updates health for plugin id.
func RecordPluginHealth(id int, enabled bool, latency time.Duration, exitCode *int, errStr, stderrTail string) {
	pluginHealthMu.Lock()
	defer pluginHealthMu.Unlock()
	e, ok := pluginHealth[id]
	if !ok {
		e = &pluginHealthEntry{}
		pluginHealth[id] = e
	}
	e.Enabled = enabled
	e.LastLatencyMs = latency.Milliseconds()
	e.LastExitCode = exitCode
	e.LastError = errStr
	e.StderrTail = stderrTail
	now := time.Now()
	e.LastInvokedAt = &now
}

// GetPluginHealth returns copy for plugin id or nil.
func GetPluginHealth(id int) *pluginHealthEntry {
	pluginHealthMu.RLock()
	defer pluginHealthMu.RUnlock()
	if e, ok := pluginHealth[id]; ok {
		cp := *e
		return &cp
	}
	return nil
}

// ResetPluginHealth clears health (tests).
func ResetPluginHealth() {
	pluginHealthMu.Lock()
	defer pluginHealthMu.Unlock()
	pluginHealth = map[int]*pluginHealthEntry{}
}

// ValidateRows checks caps and lengths.
func ValidateRows(rows []PluginRow) error {
	if len(rows) > MaxRows {
		return fmt.Errorf("too many rows: %d > %d", len(rows), MaxRows)
	}
	for i, r := range rows {
		if len(r.Label) > MaxLabelLen {
			return fmt.Errorf("row %d label too long: %d > %d", i, len(r.Label), MaxLabelLen)
		}
		if len(r.Value) > MaxValueLen {
			return fmt.Errorf("row %d value too long: %d > %d", i, len(r.Value), MaxValueLen)
		}
		if len(r.Text) > MaxTextLen {
			return fmt.Errorf("row %d text too long: %d > %d", i, len(r.Text), MaxTextLen)
		}
	}
	return nil
}

// ValidatePNG validates base64 PNG dimensions.
func ValidatePNG(b64 string, wantW, wantH int) ([]byte, error) {
	if len(b64) > MaxPNGB64Bytes {
		return nil, fmt.Errorf("png_b64 too large: %d > %d", len(b64), MaxPNGB64Bytes)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		// try raw URL variant
		raw2, err2 := base64.StdEncoding.DecodeString(b64)
		if err2 != nil {
			return nil, fmt.Errorf("invalid base64: %w", err)
		}
		raw = raw2
	}
	if len(raw) < 8 || string(raw[0:8]) != "\x89PNG\r\n\x1a\n" {
		return nil, fmt.Errorf("not a PNG (bad magic)")
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("png decode failed: %w", err)
	}
	if cfg.Width != wantW || cfg.Height != wantH {
		return nil, fmt.Errorf("png dimensions %dx%d != requested %dx%d", cfg.Width, cfg.Height, wantW, wantH)
	}
	return raw, nil
}

// strict decode helper
func strictDecode(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// ParsePluginResponse parses stdout JSON strictly, enforcing v:1 and exclusive variant.
func ParsePluginResponse(data []byte, wantW, wantH int) (*PluginResponse, error) {
	var ver struct {
		V *int `json:"v"`
	}
	if err := json.Unmarshal(data, &ver); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if ver.V == nil || *ver.V != PluginVersion {
		return nil, fmt.Errorf("wrong version: want %d", PluginVersion)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	_, hasRows := raw["rows"]
	_, hasPNG := raw["png_b64"]
	if hasRows && hasPNG {
		return nil, fmt.Errorf("response must have either rows or png_b64, not both")
	}
	if hasRows {
		var r PluginRowsResponse
		if err := strictDecode(data, &r); err != nil {
			return nil, err
		}
		if err := ValidateRows(r.Rows); err != nil {
			return nil, err
		}
		ttl := 0
		if r.TTLSeconds != nil {
			ttl = *r.TTLSeconds
		}
		return &PluginResponse{Rows: r.Rows, TTL: ttl, IsPNG: false}, nil
	}
	if hasPNG {
		var p PluginPNGResponse
		if err := strictDecode(data, &p); err != nil {
			return nil, err
		}
		if p.PNGB64 == "" {
			return nil, fmt.Errorf("png_b64 empty")
		}
		if _, err := ValidatePNG(p.PNGB64, wantW, wantH); err != nil {
			return nil, err
		}
		ttl := 0
		if p.TTLSeconds != nil {
			ttl = *p.TTLSeconds
		}
		return &PluginResponse{PNGB64: p.PNGB64, TTL: ttl, IsPNG: true}, nil
	}
	return nil, fmt.Errorf("response must contain rows or png_b64")
}

// IsLocalhostHost checks literal host allow-list.
func IsLocalhostHost(host string) bool {
	h := strings.ToLower(host)
	// strip port if any
	if strings.Contains(h, ":") {
		// handle IPv6 bracket
		if strings.HasPrefix(h, "[") {
			end := strings.Index(h, "]")
			if end != -1 {
				h = h[1:end]
			}
		} else {
			// split host:port but careful for ::1 without brackets? ::1 is bare
			if h != "::1" {
				if idx := strings.LastIndex(h, ":"); idx != -1 {
					h = h[:idx]
				}
			}
		}
	}
	// remove brackets
	h = strings.Trim(h, "[]")
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
}

// ValidateHTTPTarget checks host allow-list.
func ValidateHTTPTarget(target string) error {
	u, err := url.Parse(target)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("http plugin URL must be http or https")
	}
	if !IsLocalhostHost(u.Host) {
		return fmt.Errorf("http plugin target must be localhost, 127.0.0.1 or ::1")
	}
	return nil
}

// transport results
type transportResult struct {
	stdout   []byte
	stderr   string
	exitCode *int
	latency  time.Duration
	err      error
}

func execTransport(ctx context.Context, target string, reqBody []byte) transportResult {
	start := time.Now()
	cmd := exec.CommandContext(ctx, target)
	cmd.Stdin = bytes.NewReader(reqBody)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	latency := time.Since(start)
	stderrStr := stderr.String()
	if len(stderrStr) > StderrCap {
		stderrStr = stderrStr[len(stderrStr)-StderrCap:]
	}
	var exitCode *int
	if cmd.ProcessState != nil {
		ec := cmd.ProcessState.ExitCode()
		exitCode = &ec
	}
	if err != nil && ctx.Err() != nil {
		// timeout/cancelled
		return transportResult{stderr: stderrStr, exitCode: exitCode, latency: latency, err: ctx.Err()}
	}
	if err != nil && exitCode != nil && len(stdout.Bytes()) == 0 {
		// still return stderr but propagate error if no output?
	}
	return transportResult{stdout: stdout.Bytes(), stderr: stderrStr, exitCode: exitCode, latency: latency, err: err}
}

var httpTransportClient = &http.Client{
	Timeout: 0, // we use context timeout
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func httpTransport(ctx context.Context, target string, reqBody []byte) transportResult {
	start := time.Now()
	if err := ValidateHTTPTarget(target); err != nil {
		return transportResult{latency: time.Since(start), err: err}
	}
	req, err := http.NewRequestWithContext(ctx, "POST", target, bytes.NewReader(reqBody))
	if err != nil {
		return transportResult{latency: time.Since(start), err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "LEDit-plugin-client/1")
	resp, err := httpTransportClient.Do(req)
	latency := time.Since(start)
	if err != nil {
		return transportResult{latency: latency, err: err}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	var stderrTail string
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		stderrTail = fmt.Sprintf("http status %d", resp.StatusCode)
	}
	exit := resp.StatusCode
	ec := exit
	return transportResult{stdout: body, stderr: stderrTail, exitCode: &ec, latency: latency}
}

// InvokePlugin dispatches by kind with per-invocation timeout.
func InvokePlugin(ctx context.Context, plugin PluginInfo, req PluginRequest) (*PluginResponse, transportResult) {
	timeout := plugin.TimeoutMs
	if timeout <= 0 {
		timeout = 3000
	}
	ctx2, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
	defer cancel()

	req.V = PluginVersion
	reqBody, _ := json.Marshal(req)

	var tr transportResult
	switch plugin.Kind {
	case "exec":
		tr = execTransport(ctx2, plugin.Target, reqBody)
	case "http":
		tr = httpTransport(ctx2, plugin.Target, reqBody)
	default:
		tr = transportResult{err: fmt.Errorf("unknown plugin kind %q", plugin.Kind)}
	}
	// record health regardless; caller also records success/failure
	if tr.err != nil {
		// check context timeout
		if ctx2.Err() == context.DeadlineExceeded {
			tr.err = fmt.Errorf("plugin timeout after %dms: %w", timeout, tr.err)
		}
		return nil, tr
	}
	// parse response
	resp, err := ParsePluginResponse(tr.stdout, req.Width, req.Height)
	if err != nil {
		tr.err = err
		return nil, tr
	}
	return resp, tr
}

// PluginSource adapter for rendering
type PluginSource struct {
	PluginID int
	Config   json.RawMessage
	Width    int
	Height   int
	DeviceID int
	// Fetcher allows injection for tests.
	Fetcher func(ctx context.Context, id int, req PluginRequest) (*PluginResponse, transportResult, error)
}

func (p *PluginSource) GetPNG(width, height int) (*render.RenderedImage, error) {
	return p.GetPNGWithContext(context.Background(), width, height)
}

func (p *PluginSource) GetPNGWithContext(ctx context.Context, width, height int) (*render.RenderedImage, error) {
	cfg := p.Config
	if len(cfg) == 0 {
		cfg = json.RawMessage(`{}`)
	}
	req := PluginRequest{
		Config:    cfg,
		Width:     width,
		Height:    height,
		Timestamp: time.Now().Format(time.RFC3339),
		DeviceID:  p.DeviceID,
	}
	var resp *PluginResponse
	var tr transportResult
	var err error
	if p.Fetcher != nil {
		resp, tr, err = p.Fetcher(ctx, p.PluginID, req)
		if err != nil {
			return nil, err
		}
		if tr.err != nil {
			return nil, tr.err
		}
	} else {
		// fallback: not configured, return error so caller can degrade
		return nil, fmt.Errorf("plugin fetcher not configured")
	}
	if resp.IsPNG {
		raw, err := ValidatePNG(resp.PNGB64, width, height)
		if err != nil {
			return nil, err
		}
		return &render.RenderedImage{Format: "PNG", Data: raw}, nil
	}
	// rows -> render via GenericAPI theme path
	data := map[string]string{}
	for i, row := range resp.Rows {
		key := row.Label
		if key == "" {
			key = fmt.Sprintf("%d", i+1)
		}
		val := row.Value
		if val == "" {
			val = row.Text
		}
		if val == "" {
			val = "-"
		}
		if len(val) > 28 {
			val = val[:28] + "..."
		}
		data[key] = val
	}
	if len(data) == 0 {
		data["status"] = "no data"
	}
	return render.RenderDict(data, width, height, DefaultTheme(), "fonts/PixelifySans.ttf")
}
