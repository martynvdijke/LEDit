package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"ledit/ent"
)

// MQTTController manages the MQTT control-plane connection.
type MQTTController struct {
	s       *Server
	cfg     *ent.MQTTSettings
	client  mqtt.Client
	stop    chan struct{}
	stopped chan struct{}
	once    sync.Once
}

// mqttNewClient is a seam for tests to inject a fake client.
var mqttNewClient = func(opts *mqtt.ClientOptions) mqtt.Client {
	return mqtt.NewClient(opts)
}

// BackoffDelay returns exponential backoff capped at 60s.
func BackoffDelay(attempt int) time.Duration {
	d := time.Duration(1<<attempt) * time.Second
	if d > 60*time.Second {
		return 60 * time.Second
	}
	return d
}

func backoffDelay(attempt int) time.Duration { return BackoffDelay(attempt) }

// LoadMQTTSettings queries the single MQTTSettings row. Returns nil if none.
func LoadMQTTSettings(s *Server) *ent.MQTTSettings {
	if s == nil || s.DB == nil {
		return nil
	}
	m, err := s.DB.MQTTSettings.Query().First(s.Ctx)
	if err != nil {
		return nil
	}
	return m
}

// StartMQTT connects to the broker when enabled and broker is non-empty.
// Returns nil silently when disabled/broker empty or on nil input.
// Startup connect failure is non-fatal: logs a warning and retries in background.
func StartMQTT(s *Server) *MQTTController {
	if s == nil {
		return nil
	}
	cfg := LoadMQTTSettings(s)
	if cfg == nil || !cfg.Enabled || strings.TrimSpace(cfg.Broker) == "" {
		return nil
	}
	ctrl := &MQTTController{
		s:       s,
		cfg:     cfg,
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	opts := mqtt.NewClientOptions()
	opts.AddBroker(cfg.Broker)
	opts.SetClientID(fmt.Sprintf("ledit-%d", time.Now().UnixNano()%1000000))
	opts.SetCleanSession(true)
	opts.SetAutoReconnect(false)
	if cfg.Username != "" {
		opts.SetUsername(cfg.Username)
		if cfg.Password != "" {
			opts.SetPassword(cfg.Password)
		}
	}
	opts.SetConnectionLostHandler(func(_ mqtt.Client, err error) {
		slog.Warn("mqtt connection lost", "error", err)
		go ctrl.reconnect(opts)
	})

	client := mqttNewClient(opts)
	ctrl.client = client

	token := client.Connect()
	// Wait with timeout so startup never blocks indefinitely.
	if !token.WaitTimeout(10 * time.Second) {
		slog.Warn("mqtt initial connect timeout", "broker", cfg.Broker)
		go ctrl.reconnect(opts)
		return ctrl
	}
	if err := token.Error(); err != nil {
		slog.Warn("mqtt initial connect failed", "broker", cfg.Broker, "error", err)
		go ctrl.reconnect(opts)
		return ctrl
	}

	if err := ctrl.subscribeAll(); err != nil {
		slog.Warn("mqtt subscribe failed", "error", err)
	}

	return ctrl
}

func (c *MQTTController) subscribeAll() error {
	if c.client == nil || c.cfg == nil {
		return nil
	}
	// Control topic
	if topic := strings.TrimSpace(c.cfg.ControlTopic); topic != "" {
		tok := c.client.Subscribe(topic, 0, func(_ mqtt.Client, msg mqtt.Message) {
			c.handleControlPayload(string(msg.Payload()))
		})
		if tok.WaitTimeout(5*time.Second) && tok.Error() != nil {
			return tok.Error()
		}
	}
	// Display topic
	if topic := strings.TrimSpace(c.cfg.DisplayTopic); topic != "" {
		tok := c.client.Subscribe(topic, 0, func(_ mqtt.Client, msg mqtt.Message) {
			c.handleDisplayPayload(string(msg.Payload()))
		})
		if tok.WaitTimeout(5*time.Second) && tok.Error() != nil {
			return tok.Error()
		}
	}
	// NL control topic (optional, when AI configured)
	if c.s != nil {
		if _, ok := LoadAIConfig(c.s); ok {
			tok := c.client.Subscribe("ledit/control/nl", 1, func(_ mqtt.Client, msg mqtt.Message) {
				c.handleNLPayload(string(msg.Payload()))
			})
			if tok.WaitTimeout(5*time.Second) && tok.Error() != nil {
				return tok.Error()
			}
		}
	}
	return nil
}

func (c *MQTTController) reconnect(opts *mqtt.ClientOptions) {
	// Avoid spawning multiple reconnect loops concurrently: simple attempt loop until success or stop.
	for attempt := 0; ; attempt++ {
		select {
		case <-c.stop:
			return
		default:
		}
		d := BackoffDelay(attempt)
		select {
		case <-c.stop:
			return
		case <-time.After(d):
		}
		// If already connected, ensure subscriptions then exit.
		if c.client != nil && c.client.IsConnected() {
			_ = c.subscribeAll()
			return
		}
		// For real paho client we need to create a new client if previous Connect failed?
		// Reuse existing client Connect.
		tok := c.client.Connect()
		tok.WaitTimeout(10 * time.Second)
		if tok.Error() == nil && c.client.IsConnected() {
			slog.Info("mqtt reconnected", "broker", c.cfg.Broker)
			_ = c.subscribeAll()
			return
		}
		slog.Warn("mqtt reconnect failed", "attempt", attempt, "error", tok.Error())
	}
}

// handleControlPayload is internal; exported wrapper below for tests.
func (c *MQTTController) handleControlPayload(payload string) {
	HandleControlPayload(payload)
}

func (c *MQTTController) handleNLPayload(payload string) {
	text := strings.TrimSpace(payload)
	if text == "" {
		return
	}
	cfg, ok := LoadAIConfig(c.s)
	if !ok {
		slog.Debug("mqtt nl ignored: AI not configured")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	intent, err := ParseIntent(ctx, text, cfg)
	if err != nil {
		slog.Warn("mqtt nl parse failed", "payload", text, "error", err)
		return
	}
	reply := ExecuteIntent(c.s, intent)
	slog.Info("mqtt nl executed", "payload", text, "action", intent.Action, "reply", reply)
}

// HandleNLPayload is exported for tests: executes NL payload via same parser, no reply publish.
func HandleNLPayload(s *Server, payload string) {
	text := strings.TrimSpace(payload)
	if text == "" {
		return
	}
	cfg, ok := LoadAIConfig(s)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	intent, err := ParseIntent(ctx, text, cfg)
	if err != nil {
		slog.Debug("mqtt nl parse failed", "error", err)
		return
	}
	ExecuteIntent(s, intent)
}

// HandleControlPayload maps control payloads to GlobalFeed actions.
// Exported for testability without a broker.
func HandleControlPayload(payload string) {
	p := strings.TrimSpace(payload)
	switch p {
	case "next", "skip":
		GlobalFeed.Next()
	case "pause":
		GlobalFeed.Pause()
	case "resume":
		GlobalFeed.Resume()
	default:
		slog.Debug("unknown mqtt control payload", "payload", p)
	}
}

// handleDisplayPayload handles display topic messages.
func (c *MQTTController) handleDisplayPayload(payload string) {
	HandleDisplayPayload(c.s, payload)
}

// HandleDisplayPayload pushes a notification for non-empty payload.
// Exported for testability.
func HandleDisplayPayload(s *Server, payload string) {
	if strings.TrimSpace(payload) == "" {
		return
	}
	s.AddNotification(payload, "", WithTTL(time.Duration(s.webhookDefaultTTL())*time.Second))
}

// Stop disconnects and closes channels idempotently.
func (c *MQTTController) Stop() {
	if c == nil {
		return
	}
	c.once.Do(func() {
		close(c.stop)
		if c.client != nil && c.client.IsConnected() {
			c.client.Disconnect(250)
		}
		close(c.stopped)
	})
}

// RestartWithSettings stops the current controller and starts a new one with fresh settings.
func (c *MQTTController) RestartWithSettings(s *Server) *MQTTController {
	if c != nil {
		c.Stop()
	}
	return StartMQTT(s)
}

var mqttCtrlGlobal *MQTTController

func SetGlobalMqttCtrl(c *MQTTController) { mqttCtrlGlobal = c }

func PublishOutbound(topic, payload string, retained bool) {
	if mqttCtrlGlobal == nil || mqttCtrlGlobal.client == nil || !mqttCtrlGlobal.client.IsConnected() {
		return
	}
	mqttCtrlGlobal.client.Publish(topic, 0, retained, payload)
}

func PublishHASSDiscovery(deviceID int) {
	topic := fmt.Sprintf("homeassistant/sensor/ledit_%d_online/config", deviceID)
	payload := fmt.Sprintf(`{"name":"LEDit %d online","state_topic":"ledit/device/%d/online","device":{"identifiers":["ledit_%d"]}}`, deviceID, deviceID, deviceID)
	PublishOutbound(topic, payload, true)
}

func ClearDeviceMqtt(deviceID int) {
	topic := fmt.Sprintf("ledit/device/%d/online", deviceID)
	PublishOutbound(topic, "", true)
}

// RestartMQTT is a package-level helper for admin settings-change path when no controller is retained.
func RestartMQTT(existing *MQTTController, s *Server) *MQTTController {
	if existing != nil {
		existing.Stop()
	}
	return StartMQTT(s)
}
