package handlers

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"
)

// OutputFrame is a mapped pixel frame ready to send.
type OutputFrame struct {
	Width, Height int
	Pixels        []byte
	Seq           uint32
	At            time.Time
}

// OutputSink pushes frames to a target.
type OutputSink interface {
	Send(f OutputFrame) error
	Name() string
	Close() error
}

// OutputSinkConfig configures a sink.
type OutputSinkConfig struct {
	Transport   string
	Host        string
	Port        int
	WLEDMode    string
	WLEDChannel int
	Universe    int
	FPS         int
	ColorOrder  string
	Gamma       float64
	Serpentine  bool
	Username    string
	Password    string
}

// Interval returns clamped fps interval.
func (c OutputSinkConfig) Interval() time.Duration {
	fps := c.FPS
	if fps < 1 {
		fps = 1
	}
	if fps > 60 {
		fps = 60
	}
	return time.Second / time.Duration(fps)
}

func clampedFPS(fps int) int {
	if fps < 1 {
		return 1
	}
	if fps > 60 {
		return 60
	}
	return fps
}

// NewOutputSink creates a sink per transport.
func NewOutputSink(cfg OutputSinkConfig) (OutputSink, error) {
	switch cfg.Transport {
	case "wled":
		return newWLEDSink(cfg)
	case "artnet":
		port := cfg.Port
		if port == 0 {
			port = 6454
		}
		if cfg.Host == "" {
			cfg.Host = "127.0.0.1"
		}
		perU := 512
		return &artNetSink{host: cfg.Host, port: port, startU: cfg.Universe, perU: perU}, nil
	default:
		return nil, fmt.Errorf("unknown transport %q", cfg.Transport)
	}
}

func newWLEDSink(cfg OutputSinkConfig) (OutputSink, error) {
	host := cfg.Host
	if host == "" {
		host = "127.0.0.1"
	}
	mode := cfg.WLEDMode
	if mode == "" {
		mode = "ddp"
	}
	switch mode {
	case "ddp":
		port := cfg.Port
		if port == 0 {
			port = 4048
		}
		return &wledDDPSink{host: host, port: port}, nil
	case "e131":
		port := cfg.Port
		if port == 0 {
			port = 5568
		}
		perU := 512
		return &artNetSink{host: host, port: port, startU: cfg.Universe, perU: perU, useSACN: true}, nil
	case "http":
		port := cfg.Port
		if port == 0 {
			port = 80
		}
		return &wledHTTPSink{host: host, port: port, username: cfg.Username, password: cfg.Password, client: &http.Client{Timeout: 2 * time.Second}}, nil
	default:
		return nil, fmt.Errorf("unknown wled mode %q", mode)
	}
}

// DDP sink
type wledDDPSink struct {
	host string
	port int
	seq  uint8
}

func (w *wledDDPSink) Name() string { return "wled-ddp" }
func (w *wledDDPSink) Close() error { return nil }
func (w *wledDDPSink) Send(f OutputFrame) error {
	addr := net.JoinHostPort(w.host, fmt.Sprintf("%d", w.port))
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return err
	}
	// Split into 1440-byte chunks.
	const maxChunk = 1440
	data := f.Pixels
	if len(data) == 0 {
		return nil
	}
	for offset := 0; offset < len(data); offset += maxChunk {
		end := offset + maxChunk
		if end > len(data) {
			end = len(data)
		}
		chunk := data[offset:end]
		w.seq++
		pkt := buildDDPPacket(w.seq, uint32(offset), chunk)
		c, err := net.DialUDP("udp", nil, udpAddr)
		if err != nil {
			return err
		}
		_, err = c.Write(pkt)
		c.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func buildDDPPacket(seq uint8, offset uint32, data []byte) []byte {
	hdr := make([]byte, 10)
	hdr[0] = 0x41 // version 1 | push
	hdr[1] = seq
	hdr[2] = 0x00 // data type RGB
	hdr[3] = 0x00 // source id
	// bytes 4-7 offset
	binary.BigEndian.PutUint32(hdr[4:8], offset)
	binary.BigEndian.PutUint16(hdr[8:10], uint16(len(data)))
	return append(hdr, data...)
}

// HTTP sink
type wledHTTPSink struct {
	host     string
	port     int
	username string
	password string
	client   *http.Client
}

func (w *wledHTTPSink) Name() string { return "wled-http" }
func (w *wledHTTPSink) Close() error { return nil }
func (w *wledHTTPSink) Send(f OutputFrame) error {
	ints := make([]int, len(f.Pixels))
	for i, b := range f.Pixels {
		ints[i] = int(b)
	}
	body, _ := json.Marshal(map[string]any{
		"on":  true,
		"seg": []map[string]any{{"i": ints}},
	})
	url := fmt.Sprintf("http://%s:%d/json/state", w.host, w.port)
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if w.username != "" {
		req.SetBasicAuth(w.username, w.password)
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("wled http %d", resp.StatusCode)
	}
	return nil
}
