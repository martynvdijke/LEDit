package handlers

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"ledit/ent"
)

type FeedController struct {
	mu          sync.Mutex
	Paused      bool
	Skip        bool
	CurrentName string
	NextName    string
	PinnedKey   string
	PinnedBy    string
}

var GlobalFeed = &FeedController{}

var (
	deviceFeedMu sync.RWMutex
	deviceFeeds  = map[int]*FeedController{}
)

func registerDeviceFeed(deviceID int, fc *FeedController) {
	deviceFeedMu.Lock()
	defer deviceFeedMu.Unlock()
	deviceFeeds[deviceID] = fc
}
func unregisterDeviceFeed(deviceID int) {
	deviceFeedMu.Lock()
	defer deviceFeedMu.Unlock()
	delete(deviceFeeds, deviceID)
}
func getDeviceFeed(deviceID int) (*FeedController, bool) {
	deviceFeedMu.RLock()
	defer deviceFeedMu.RUnlock()
	fc, ok := deviceFeeds[deviceID]
	return fc, ok
}

func (fc *FeedController) IsPaused() bool {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	return fc.Paused
}

func (fc *FeedController) ShouldSkip() bool {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if fc.Skip {
		fc.Skip = false
		return true
	}
	return false
}

func (fc *FeedController) Pause() {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.Paused = true
	GlobalBus.Emit(Event{Type: EventFeedPaused, Timestamp: time.Now()})
}

func (fc *FeedController) Resume() {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.Paused = false
	GlobalBus.Emit(Event{Type: EventFeedResumed, Timestamp: time.Now()})
}

func (fc *FeedController) Next() {
	fc.mu.Lock()
	cur := fc.CurrentName
	fc.Skip = true
	fc.PinnedKey = ""
	fc.PinnedBy = ""
	fc.mu.Unlock()
	// attribute skip
	if cur != "" {
		RecordSkip("", 0, cur)
	} else {
		RecordSkip("", 0, "")
	}
	triggerSkipRecompute()
}

func (fc *FeedController) Pin(key, by string) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.PinnedKey = key
	fc.PinnedBy = by
}

func (fc *FeedController) Unpin() {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.PinnedKey = ""
	fc.PinnedBy = ""
}

func (fc *FeedController) IsPinned() (string, string, bool) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if fc.PinnedKey == "" {
		return "", "", false
	}
	return fc.PinnedKey, fc.PinnedBy, true
}

func (fc *FeedController) SetCurrent(name, next string) {
	fc.mu.Lock()
	prev := fc.CurrentName
	fc.CurrentName = name
	fc.NextName = next
	fc.mu.Unlock()
	if prev != "" && prev != name {
		GlobalBus.Emit(Event{Type: EventSourceChanged, Timestamp: time.Now(), Data: map[string]string{"from": prev, "to": name}})
	}
	return
}

func (fc *FeedController) Status() map[string]any {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	m := map[string]any{
		"paused":  fc.Paused,
		"current": fc.CurrentName,
		"next":    fc.NextName,
	}
	if fc.PinnedKey != "" {
		m["pinned_by"] = fc.PinnedBy
		m["pinned_key"] = fc.PinnedKey
	}
	return m
}

// API handlers

func (s *Server) APIFeedStatus(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Vary", "Cookie, Authorization")
	c.JSON(http.StatusOK, GlobalFeed.Status())
}

func (s *Server) APIFeedNext(c *gin.Context) {
	GlobalFeed.Next()
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Server) APIFeedPause(c *gin.Context) {
	GlobalFeed.Pause()
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Server) APIFeedResume(c *gin.Context) {
	GlobalFeed.Resume()
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Server) APIFeedPriority(c *gin.Context) {
	var req priorityMsg
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	s.AddNotification(req.Title, req.Message)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Server) APIWebhookNotify(c *gin.Context) {
	var req priorityMsg
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	s.AddNotification(req.Title, req.Message)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Server) APINotificationHistory(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Vary", "Cookie, Authorization")
	c.JSON(http.StatusOK, s.GetNotificationHistory())
}

func (s *Server) AdminNotifications(c *gin.Context) {
	s.renderPage(c, http.StatusOK, "notifications.html", gin.H{
		"notifications": s.GetNotificationHistory(),
	})
}

type priorityMsg struct {
	Title   string `json:"title"`
	Message string `json:"message"`
}

var (
	priorityMu   sync.Mutex
	notifHistory []notifEntry
	notifID      int
)

type notifEntry struct {
	ID        int       `json:"id"`
	Title     string    `json:"title"`
	Message   string    `json:"message"`
	Time      string    `json:"time"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
	// Color is an in-memory hint reserved for future theme support; DB persistence stays Title/Message-only.
	Color string `json:"color,omitempty"`
}

// NotifOption configures AddNotification.
type notifConfig struct {
	ttl       time.Duration
	color     string
	expiresAt time.Time
}

// NotifOption is an exported functional option for AddNotification.
type NotifOption func(*notifConfig)

// WithTTL sets the notification TTL. Zero or negative means no expiry.
func WithTTL(d time.Duration) NotifOption {
	return func(c *notifConfig) { c.ttl = d }
}

// withColor is an internal option to store a color hint.
func withColor(color string) NotifOption {
	return func(c *notifConfig) { c.color = color }
}

// withExpiresAt is an internal option for testing expiry.
func withExpiresAt(t time.Time) NotifOption {
	return func(c *notifConfig) { c.expiresAt = t }
}

// addToMemoryQueue stores a notification in the in-memory queue (for live feed display).
func addToMemoryQueue(title, message string) {
	addToMemoryQueueWithOptions(title, message)
}

func addToMemoryQueueWithOptions(title, message string, opts ...NotifOption) {
	cfg := &notifConfig{}
	for _, o := range opts {
		o(cfg)
	}
	var exp time.Time
	if !cfg.expiresAt.IsZero() {
		exp = cfg.expiresAt
	} else if cfg.ttl > 0 {
		exp = time.Now().Add(cfg.ttl)
	}
	priorityMu.Lock()
	defer priorityMu.Unlock()
	notifID++
	t := time.Now().Format("15:04:05")
	notifHistory = append(notifHistory, notifEntry{
		ID:        notifID,
		Title:     title,
		Message:   message,
		Time:      t,
		ExpiresAt: exp,
		Color:     cfg.color,
	})
	// Keep last 50
	if len(notifHistory) > 50 {
		notifHistory = notifHistory[len(notifHistory)-50:]
	}
}

// getMemoryQueue returns a copy of the in-memory notification queue.
func getMemoryQueue() []notifEntry {
	priorityMu.Lock()
	defer priorityMu.Unlock()
	out := make([]notifEntry, len(notifHistory))
	copy(out, notifHistory)
	return out
}

// CurrentNotifSeq returns the current notification sequence number.
func CurrentNotifSeq() int {
	priorityMu.Lock()
	defer priorityMu.Unlock()
	return notifID
}

// NotificationsAfter returns in-memory notifications with an ID greater than
// cursor, in ascending order. Used to broadcast each notification to every
// connection exactly once. Expired entries are pruned on read.
func NotificationsAfter(cursor int) []notifEntry {
	priorityMu.Lock()
	defer priorityMu.Unlock()
	now := time.Now()
	// Prune expired entries.
	kept := notifHistory[:0]
	for _, n := range notifHistory {
		if !n.ExpiresAt.IsZero() && n.ExpiresAt.Before(now) {
			continue
		}
		kept = append(kept, n)
	}
	notifHistory = kept
	var out []notifEntry
	for _, n := range notifHistory {
		if n.ID > cursor {
			out = append(out, n)
		}
	}
	return out
}

// AddNotification persists a notification to DB and adds to the in-memory queue.
// Variadic opts keep existing call sites compiling unchanged. TTL via WithTTL.
func (s *Server) AddNotification(title, message string, opts ...NotifOption) {
	if s.DB != nil {
		s.DB.Notification.Create().SetTitle(title).SetMessage(message).SetCreatedAt(time.Now()).SaveX(s.Ctx)
	}
	addToMemoryQueueWithOptions(title, message, opts...)
	GlobalBus.Emit(Event{Type: EventNotificationFired, Timestamp: time.Now(), Data: map[string]any{"title": title, "message": message}})
}

// GetNotificationHistory returns merged DB + in-memory notification history (up to 50).
func (s *Server) GetNotificationHistory() []notifEntry {
	memQueue := getMemoryQueue()

	// Also load from DB
	dbNotifs, err := s.DB.Notification.Query().Order(ent.Desc("created_at")).Limit(50).All(s.Ctx)
	if err != nil || len(dbNotifs) == 0 {
		return memQueue
	}

	// Merge DB entries not already in memory
	existing := map[int]bool{}
	for _, n := range memQueue {
		existing[n.ID] = true
	}
	var merged []notifEntry
	for _, dn := range dbNotifs {
		if !existing[dn.ID] {
			merged = append(merged, notifEntry{
				ID:      dn.ID,
				Title:   dn.Title,
				Message: dn.Message,
				Time:    dn.CreatedAt.Format("15:04:05"),
			})
		}
	}
	merged = append(merged, memQueue...)
	if len(merged) > 50 {
		merged = merged[:50]
	}
	return merged
}
