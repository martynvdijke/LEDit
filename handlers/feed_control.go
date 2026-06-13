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
}

var GlobalFeed = &FeedController{}

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
}

func (fc *FeedController) Resume() {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.Paused = false
}

func (fc *FeedController) Next() {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.Skip = true
}

func (fc *FeedController) SetCurrent(name, next string) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.CurrentName = name
	fc.NextName = next
}

func (fc *FeedController) Status() map[string]any {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	return map[string]any{
		"paused":  fc.Paused,
		"current": fc.CurrentName,
		"next":    fc.NextName,
	}
}

// API handlers

func (s *Server) APIFeedStatus(c *gin.Context) {
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
	priorityMu.Lock()
	priorityMessages = append(priorityMessages, req)
	priorityMu.Unlock()
	s.AddNotification(req.Title, req.Message)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Server) APIWebhookNotify(c *gin.Context) {
	var req priorityMsg
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	priorityMu.Lock()
	priorityMessages = append(priorityMessages, req)
	priorityMu.Unlock()
	s.AddNotification(req.Title, req.Message)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Server) APINotificationHistory(c *gin.Context) {
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
	priorityMu       sync.Mutex
	priorityMessages []priorityMsg
	notifHistory     []notifEntry
	notifID          int
)

type notifEntry struct {
	ID      int    `json:"id"`
	Title   string `json:"title"`
	Message string `json:"message"`
	Time    string `json:"time"`
}

func PopPriorityMessage() *priorityMsg {
	priorityMu.Lock()
	defer priorityMu.Unlock()
	if len(priorityMessages) == 0 {
		return nil
	}
	msg := priorityMessages[0]
	priorityMessages = priorityMessages[1:]
	return &msg
}

// addToMemoryQueue stores a notification in the in-memory queue (for live feed display).
func addToMemoryQueue(title, message string) {
	priorityMu.Lock()
	defer priorityMu.Unlock()
	notifID++
	t := time.Now().Format("15:04:05")
	notifHistory = append(notifHistory, notifEntry{
		ID:      notifID,
		Title:   title,
		Message: message,
		Time:    t,
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

// AddNotification persists a notification to DB and adds to the in-memory queue.
func (s *Server) AddNotification(title, message string) {
	if s.DB != nil {
		s.DB.Notification.Create().SetTitle(title).SetMessage(message).SetCreatedAt(time.Now()).SaveX(s.Ctx)
	}
	addToMemoryQueue(title, message)
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
