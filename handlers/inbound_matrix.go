package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var matrixHTTPClient = &http.Client{Timeout: 35 * time.Second}

func matrixSyncURL(homeserver, since string) string {
	base := strings.TrimRight(homeserver, "/")
	u := base + "/_matrix/client/v3/sync?timeout=30000"
	if since != "" {
		u += "&since=" + url.QueryEscape(since)
	}
	return u
}

type matrixEvent struct {
	RoomID  string
	EventID string
	Type    string
	MsgType string
	Body    string
}

func matrixParseSync(body []byte) (string, []matrixEvent, error) {
	var raw struct {
		NextBatch string `json:"next_batch"`
		Rooms     struct {
			Join map[string]struct {
				Timeline struct {
					Events []struct {
						EventID string `json:"event_id"`
						Type    string `json:"type"`
						Content struct {
							MsgType string `json:"msgtype"`
							Body    string `json:"body"`
						} `json:"content"`
					} `json:"events"`
				} `json:"timeline"`
			} `json:"join"`
		} `json:"rooms"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return "", nil, err
	}
	var evs []matrixEvent
	for roomID, j := range raw.Rooms.Join {
		for _, e := range j.Timeline.Events {
			if e.Type != "m.room.message" {
				continue
			}
			if e.Content.MsgType != "m.text" {
				continue
			}
			if strings.TrimSpace(e.Content.Body) == "" {
				continue
			}
			evs = append(evs, matrixEvent{
				RoomID:  roomID,
				EventID: e.EventID,
				Type:    e.Type,
				MsgType: e.Content.MsgType,
				Body:    e.Content.Body,
			})
		}
	}
	return raw.NextBatch, evs, nil
}

type MatrixInbound struct {
	s          *Server
	cfg        InboundAdapterConfig
	homeserver string
	token      string
	since      string
	rowID      int
	stop       chan struct{}
	stopped    chan struct{}
	mu         sync.Mutex
	seen       map[string]bool
	order      []string
}

func init() { registerInboundFactory("matrix", newMatrixInbound) }

func newMatrixInbound(s *Server, cfg InboundAdapterConfig) InboundAdapter {
	hs := ""
	if cfg.Config != nil {
		hs = strings.TrimSpace(cfg.Config["homeserver"])
	}
	tok := ""
	if cfg.Config != nil {
		tok = cfg.Config["access_token"]
	}
	if tok == "" {
		tok = cfg.Secret
	}
	since := ""
	if cfg.Config != nil {
		since = cfg.Config["since"]
	}
	m := &MatrixInbound{
		s:          s,
		cfg:        cfg,
		homeserver: hs,
		token:      tok,
		since:      since,
		stop:       make(chan struct{}),
		stopped:    make(chan struct{}),
		seen:       make(map[string]bool),
	}
	if s != nil && s.DB != nil {
		ctx := s.Ctx
		if ctx == nil {
			ctx = context.Background()
		}
		rows, err := s.DB.InboundAdapter.Query().All(ctx)
		if err == nil {
			for _, r := range rows {
				if r.Kind == "matrix" {
					m.rowID = r.ID
					break
				}
			}
		}
	}
	return m
}

func (m *MatrixInbound) Kind() string { return "matrix" }

func (m *MatrixInbound) Start() error {
	go m.loop()
	return nil
}

func (m *MatrixInbound) Stop() {
	select {
	case <-m.stop:
	default:
		close(m.stop)
	}
	<-m.stopped
}

func (m *MatrixInbound) loop() {
	defer close(m.stopped)
	attempt := 0
	for {
		select {
		case <-m.stop:
			return
		default:
		}
		if err := m.syncOnce(); err != nil {
			d := backoffDelay(attempt)
			attempt++
			select {
			case <-m.stop:
				return
			case <-time.After(d):
			}
			continue
		}
		attempt = 0
	}
}

func (m *MatrixInbound) syncOnce() error {
	if strings.TrimSpace(m.homeserver) == "" {
		return nil
	}
	u := matrixSyncURL(m.homeserver, m.since)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	if m.token != "" {
		req.Header.Set("Authorization", "Bearer "+m.token)
	}
	resp, err := matrixHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("matrix sync status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	nextBatch, events, err := matrixParseSync(body)
	if err != nil {
		return err
	}
	if nextBatch != "" {
		m.since = nextBatch
		m.persistCursor()
	}
	for _, ev := range events {
		if ev.EventID != "" && m.seenRecently(ev.EventID) {
			continue
		}
		if !inboundAllowed(m.cfg.Allowlist, ev.RoomID) {
			dropInbound("matrix", "not_allowed")
			continue
		}
		msg := InboundMessage{
			Source:     "matrix",
			SourceID:   ev.RoomID,
			SourceName: "Matrix",
			Body:       ev.Body,
			Priority:   inboundPriorityNormal,
		}
		if ok, _ := admitInbound(m.cfg, msg); !ok {
			dropInbound("matrix", "admit_failed")
			continue
		}
		if m.s != nil {
			m.s.DeliverInbound(msg)
		}
	}
	return nil
}

func (m *MatrixInbound) persistCursor() {
	if m.s == nil || m.s.DB == nil || m.rowID == 0 {
		return
	}
	ctx := m.s.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := m.s.DB.InboundAdapter.Query().All(ctx)
	if err != nil {
		return
	}
	var curConfig string
	found := false
	for _, r := range rows {
		if r.ID == m.rowID {
			curConfig = r.Config
			found = true
			break
		}
	}
	if !found {
		return
	}
	var cfg map[string]string
	_ = json.Unmarshal([]byte(curConfig), &cfg)
	if cfg == nil {
		cfg = map[string]string{}
	}
	cfg["since"] = m.since
	if cfg["homeserver"] == "" && m.homeserver != "" {
		cfg["homeserver"] = m.homeserver
	}
	b, _ := json.Marshal(cfg)
	_ = m.s.DB.InboundAdapter.UpdateOneID(m.rowID).SetConfig(string(b)).Exec(ctx)
}

func (m *MatrixInbound) seenRecently(id string) bool {
	if id == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.seen[id] {
		return true
	}
	m.seen[id] = true
	m.order = append(m.order, id)
	if len(m.order) > 500 {
		old := m.order[0]
		m.order = m.order[1:]
		delete(m.seen, old)
	}
	return false
}
