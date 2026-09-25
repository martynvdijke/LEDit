package handlers

import (
	"context"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"ledit/ent"
	"ledit/ent/devicemessagestate"
)

// Message kinds.
const (
	MessageKindNotification = "notification"
	MessageKindIncident     = "incident"
)

// Message is the unified read model spanning notifications and incidents. It is
// computed at query time from the existing Notification/Incident records — no
// message table is written. IDs are namespaced ("notif:<id>" / "incident:<id>")
// to avoid collisions between the two ID spaces.
type Message struct {
	ID        string     `json:"id"`
	Kind      string     `json:"kind"`
	Severity  string     `json:"severity"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	Surfaces  []string   `json:"surfaces,omitempty"`
	// Priority is the inbound severity hint (0 normal .. 3 urgent). Additive:
	// omitempty keeps legacy notification/incident payloads byte-identical.
	Priority int `json:"priority,omitempty"`
	// Media optionally attaches an image for display surfaces.
	Media *MessageMedia `json:"media,omitempty"`
}

// MessageMedia is an optional image attached to a message. URL is a remote
// source, Path a local file under web/media; callers set at most one.
type MessageMedia struct {
	URL  string `json:"url,omitempty"`
	Path string `json:"path,omitempty"`
	MIME string `json:"mime,omitempty"`
}

var messageSurfaces = []string{"ws", "trmnl", "mqtt", "webhook"}

// NotificationToMessage maps an in-memory notification entry to the read model.
func NotificationToMessage(n notifEntry) Message {
	created := n.CreatedAt
	if created.IsZero() {
		created = time.Now()
	}
	var exp *time.Time
	if !n.ExpiresAt.IsZero() {
		e := n.ExpiresAt
		exp = &e
	}
	return Message{
		ID:        "notif:" + strconv.Itoa(n.ID),
		Kind:      MessageKindNotification,
		Severity:  "info",
		Title:     n.Title,
		Body:      n.Message,
		CreatedAt: created,
		ExpiresAt: exp,
		Surfaces:  messageSurfaces,
		Priority:  n.Priority,
		Media:     n.Media,
	}
}

// IncidentToMessage maps an incident to the read model. Open incidents have a
// nil ExpiresAt; resolved incidents carry their resolved_at.
func IncidentToMessage(inc *ent.Incident) Message {
	if inc == nil {
		return Message{}
	}
	var exp *time.Time
	if !inc.Active && inc.ResolvedAt != nil {
		e := *inc.ResolvedAt
		exp = &e
	}
	return Message{
		ID:        "incident:" + strconv.Itoa(inc.ID),
		Kind:      MessageKindIncident,
		Severity:  inc.Severity,
		Title:     inc.Title,
		Body:      inc.Message,
		CreatedAt: inc.CreatedAt,
		ExpiresAt: exp,
		Surfaces:  messageSurfaces,
	}
}

// ActiveMessages returns the union of unexpired notifications and open
// incidents, newest first. Derived live; nothing is persisted.
func ActiveMessages() []Message {
	now := time.Now()
	var out []Message
	for _, n := range getMemoryQueue() {
		if !n.ExpiresAt.IsZero() && !n.ExpiresAt.After(now) {
			continue
		}
		out = append(out, NotificationToMessage(n))
	}
	for _, inc := range ActiveIncidents() {
		out = append(out, IncidentToMessage(inc))
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

// MessageKindFromID returns the kind encoded in a namespaced message id, or "".
func MessageKindFromID(id string) string {
	switch {
	case strings.HasPrefix(id, "notif:"):
		return MessageKindNotification
	case strings.HasPrefix(id, "incident:"):
		return MessageKindIncident
	default:
		return ""
	}
}

// validMessageID reports whether id is a well-formed namespaced message id.
func validMessageID(id string) bool {
	switch {
	case strings.HasPrefix(id, "notif:"):
		_, err := strconv.Atoi(id[len("notif:"):])
		return err == nil
	case strings.HasPrefix(id, "incident:"):
		_, err := strconv.Atoi(id[len("incident:"):])
		return err == nil
	default:
		return false
	}
}

// recentIDs remembers recently fired message ids so a device can ack a message
// that has since resolved or expired. Bounded by a short in-memory TTL.
var recentIDs = struct {
	mu sync.Mutex
	m  map[string]time.Time
}{m: make(map[string]time.Time)}

const recentMessageIDTTL = 10 * time.Minute

func rememberMessageID(id string) {
	if id == "" {
		return
	}
	now := time.Now()
	recentIDs.mu.Lock()
	for k, t := range recentIDs.m {
		if now.Sub(t) > recentMessageIDTTL {
			delete(recentIDs.m, k)
		}
	}
	recentIDs.m[id] = now
	recentIDs.mu.Unlock()
}

func messageRecentlyActive(id string) bool {
	if !validMessageID(id) {
		return false
	}
	recentIDs.mu.Lock()
	t, ok := recentIDs.m[id]
	recentIDs.mu.Unlock()
	if ok && time.Since(t) <= recentMessageIDTTL {
		return true
	}
	for _, m := range ActiveMessages() {
		if m.ID == id {
			return true
		}
	}
	return false
}

// DeviceAck is the last message acknowledged by a device.
type DeviceAck struct {
	DeviceID    int       `json:"device_id"`
	LastAckedID string    `json:"last_acked_id"`
	LastAckedAt time.Time `json:"last_acked_at"`
}

var ackStore = struct {
	mu sync.RWMutex
	m  map[int]DeviceAck
}{m: make(map[int]DeviceAck)}

// DeviceAcks returns the per-device last-acked state, ordered by device id.
func DeviceAcks() []DeviceAck {
	ackStore.mu.RLock()
	defer ackStore.mu.RUnlock()
	out := make([]DeviceAck, 0, len(ackStore.m))
	for _, a := range ackStore.m {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DeviceID < out[j].DeviceID })
	return out
}

// recordAck ingests an optional per-device acknowledgement. Best-effort: an
// invalid or unknown id is ignored, and the delivery-log write happens off the
// feed goroutine so an ack can never block or interrupt delivery.
func recordAck(deviceID int, id string) {
	if deviceID <= 0 || !messageRecentlyActive(id) {
		return
	}
	now := time.Now()
	ackStore.mu.Lock()
	ackStore.m[deviceID] = DeviceAck{DeviceID: deviceID, LastAckedID: id, LastAckedAt: now}
	ackStore.mu.Unlock()
	if GlobalDeliveryLog != nil {
		entry := DeliveryLogEntry{
			MessageID:   id,
			Kind:        MessageKindFromID(id),
			Surface:     "ws",
			Target:      strconv.Itoa(deviceID),
			Status:      "acked",
			AttemptedAt: now,
		}
		go GlobalDeliveryLog.Record(entry)
	}
	// ponytail: best-effort persistence, never blocks feed
	go upsertDeviceMessageState(deviceID, id, "acked")
}

func recordRead(deviceID int, id string) {
	if deviceID <= 0 || !messageRecentlyActive(id) {
		return
	}
	// read is persisted but must not masquerade as an ack in DeviceAcks().
	go upsertDeviceMessageState(deviceID, id, "read")
}

func recordDismiss(deviceID int, id string) {
	if deviceID <= 0 || !messageRecentlyActive(id) {
		return
	}
	go upsertDeviceMessageState(deviceID, id, "dismissed")
}

func upsertDeviceMessageState(deviceID int, messageID, status string) {
	client := deviceStateClient()
	if client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// Try update, else create. Unique index on (device_id,message_id)
	existing, err := client.DeviceMessageState.Query().
		Where(devicemessagestate.DeviceID(deviceID), devicemessagestate.MessageIDEQ(messageID)).
		Only(ctx)
	if err == nil && existing != nil {
		_, err = client.DeviceMessageState.UpdateOneID(existing.ID).SetStatus(status).SetUpdatedAt(time.Now()).Save(ctx)
		if err != nil {
			slog.Warn("device message state update failed", "device", deviceID, "message", messageID, "error", err)
		}
		return
	}
	_, err = client.DeviceMessageState.Create().
		SetDeviceID(deviceID).SetMessageID(messageID).SetStatus(status).Save(ctx)
	if err != nil {
		// possible race: try update once more
		if existing2, err2 := client.DeviceMessageState.Query().Where(devicemessagestate.DeviceID(deviceID), devicemessagestate.MessageIDEQ(messageID)).Only(ctx); err2 == nil {
			_, _ = client.DeviceMessageState.UpdateOneID(existing2.ID).SetStatus(status).SetUpdatedAt(time.Now()).Save(ctx)
			return
		}
		slog.Warn("device message state create failed", "device", deviceID, "message", messageID, "error", err)
	}
}

func deviceStateClient() *ent.Client {
	if GlobalDeliveryLog != nil && GlobalDeliveryLog.db != nil {
		if c := GlobalDeliveryLog.db(); c != nil {
			return c
		}
	}
	if globalServerDB != nil {
		if c := globalServerDB(); c != nil {
			return c
		}
	}
	return nil
}

var globalServerDB func() *ent.Client

func messageStateFor(deviceID int, messageID string) *ent.DeviceMessageState {
	client := deviceStateClient()
	if client == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	row, _ := client.DeviceMessageState.Query().
		Where(devicemessagestate.DeviceID(deviceID), devicemessagestate.MessageIDEQ(messageID)).
		Only(ctx)
	return row
}

func DeviceMessageStates(deviceID int) []*ent.DeviceMessageState {
	client := deviceStateClient()
	if client == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	rows, _ := client.DeviceMessageState.Query().Where(devicemessagestate.DeviceID(deviceID)).All(ctx)
	return rows
}

func UnreadMessagesFor(deviceID int) []Message {
	active := ActiveMessages()
	if deviceID <= 0 {
		return active
	}
	client := deviceStateClient()
	if client == nil {
		return active
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	rows, _ := client.DeviceMessageState.Query().Where(devicemessagestate.DeviceID(deviceID)).All(ctx)
	hidden := map[string]bool{}
	for _, r := range rows {
		if r.Status == "read" || r.Status == "dismissed" {
			hidden[r.MessageID] = true
		}
	}
	if len(hidden) == 0 {
		return active
	}
	var out []Message
	for _, m := range active {
		if !hidden[m.ID] {
			out = append(out, m)
		}
	}
	return out
}

// emitMessageFired publishes a newly active message to the bus and remembers
// its id for ack validation.
func emitMessageFired(m Message) {
	if m.ID == "" {
		return
	}
	rememberMessageID(m.ID)
	rememberPublishedMessage(m)
	GlobalBus.Emit(Event{Type: EventMessageFired, Timestamp: time.Now(), Data: m})
}

// emitMessageResolved publishes a resolved/expired message to the bus.
func emitMessageResolved(m Message) {
	if m.ID == "" {
		return
	}
	forgetPublishedMessage(m.ID)
	GlobalBus.Emit(Event{Type: EventMessageResolved, Timestamp: time.Now(), Data: m})
}

// publishedMessages tracks messages whose retained MQTT state is currently
// "fired", so passively expired notifications can be tombstoned by the sweeper.
var publishedMessages = struct {
	mu sync.Mutex
	m  map[string]Message
}{m: make(map[string]Message)}

func rememberPublishedMessage(m Message) {
	// Only track while MQTT is configured: there is no retained state to
	// tombstone otherwise, and this bounds the map.
	if !mqttConnected() {
		return
	}
	publishedMessages.mu.Lock()
	if len(publishedMessages.m) < 500 {
		publishedMessages.m[m.ID] = m
	}
	publishedMessages.mu.Unlock()
}

func forgetPublishedMessage(id string) {
	publishedMessages.mu.Lock()
	delete(publishedMessages.m, id)
	publishedMessages.mu.Unlock()
}

// reconcilePublishedMessages tombstones retained state for messages that were
// published as fired but are no longer active (e.g. a notification TTL expiring
// without an explicit resolve event).
func reconcilePublishedMessages() {
	active := make(map[string]bool)
	for _, m := range ActiveMessages() {
		active[m.ID] = true
	}
	var expired []Message
	publishedMessages.mu.Lock()
	for id, m := range publishedMessages.m {
		if !active[id] {
			expired = append(expired, m)
			delete(publishedMessages.m, id)
		}
	}
	publishedMessages.mu.Unlock()
	for _, m := range expired {
		publishMessageLifecycle(m, false)
	}
}
