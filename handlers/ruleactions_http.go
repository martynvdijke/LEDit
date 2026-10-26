package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"ledit/ent"
)

// Bounded async worker for event-rule `http` then-actions. Kept out of the
// evaluator's 5s tick path: enqueue is non-blocking and delivery retries follow
// the same backoff policy as the outbound webhook sink.
const ruleHTTPQueueSize = 64

type ruleHTTPTask struct {
	url      string
	method   string
	secret   string
	event    string
	body     string
	ruleName string
}

var (
	ruleHTTPQueue    chan ruleHTTPTask
	ruleHTTPOnce     sync.Once
	ruleHTTPClient   = &http.Client{Timeout: 10 * time.Second}
	ruleHTTPSleep    = time.Sleep
	ruleHTTPBackoffs = []time.Duration{time.Second, 5 * time.Second, 25 * time.Second}
)

func enqueueRuleHTTP(rule *ent.DisplayRule, ta ThenAction) {
	if rule == nil {
		return
	}
	method := strings.ToUpper(strings.TrimSpace(ta.Method))
	if method == "" {
		method = http.MethodPost
	}
	task := ruleHTTPTask{
		url:      strings.TrimSpace(ta.URL),
		method:   method,
		secret:   ta.Secret,
		event:    strings.TrimSpace(ta.Event),
		body:     buildRuleHTTPBody(rule, ta),
		ruleName: rule.Name,
	}
	ensureRuleHTTPWorker()
	select {
	case ruleHTTPQueue <- task:
	default:
		slog.Warn("event rule http action dropped: queue full", "rule", rule.Name)
	}
}

func ensureRuleHTTPWorker() {
	ruleHTTPOnce.Do(func() {
		ruleHTTPQueue = make(chan ruleHTTPTask, ruleHTTPQueueSize)
		go func() {
			for task := range ruleHTTPQueue {
				deliverRuleHTTP(task)
			}
		}()
	})
}

func buildRuleHTTPBody(rule *ent.DisplayRule, ta ThenAction) string {
	if rule == nil {
		return ""
	}
	eventName := strings.TrimSpace(ta.Event)
	if eventName == "" {
		eventName = EventRuleTriggered
	}
	envelope := map[string]any{
		"event":     eventName,
		"timestamp": time.Now().Format(time.RFC3339),
		"data": map[string]any{
			"rule": map[string]any{
				"id":          rule.ID,
				"name":        rule.Name,
				"source_type": rule.SourceType,
				"source_id":   rule.SourceID,
			},
		},
	}
	b, err := json.Marshal(envelope)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func signRuleHTTPBody(secret string, body []byte) string {
	if secret == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func deliverRuleHTTP(task ruleHTTPTask) {
	body := []byte(task.body)
	sig := signRuleHTTPBody(task.secret, body)
	eventName := task.event
	if eventName == "" {
		eventName = EventRuleTriggered
	}

	attempts := 0
	delivered := false
	lastErr := ""
	for attempts <= len(ruleHTTPBackoffs) {
		attempts++
		req, err := http.NewRequest(task.method, task.url, strings.NewReader(task.body))
		if err != nil {
			lastErr = err.Error()
			break
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-LEDit-Event", eventName)
		if sig != "" {
			req.Header.Set("X-LEDit-Signature", sig)
		}
		resp, err := ruleHTTPClient.Do(req)
		if err != nil {
			lastErr = err.Error()
		} else {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				delivered = true
				break
			}
			lastErr = fmt.Sprintf("status %d", resp.StatusCode)
			// 4xx (except 429) are terminal; 429 and 5xx retry.
			if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < 500 {
				break
			}
		}
		if attempts > len(ruleHTTPBackoffs) {
			break
		}
		ruleHTTPSleep(ruleHTTPBackoffs[attempts-1])
	}

	status := "failed"
	if delivered {
		status = "delivered"
	}
	recordDelivery(DeliveryLogEntry{
		Kind:        "rule",
		Surface:     "webhook",
		Target:      task.url,
		Status:      status,
		AttemptedAt: time.Now(),
		Error:       lastErr,
	})
	slog.Debug("event rule http delivered", "rule", task.ruleName, "url", task.url, "status", status)
}
