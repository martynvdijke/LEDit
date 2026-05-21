package handlers

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type FeedController struct {
	mu           sync.Mutex
	Paused       bool
	Skip         bool
	CurrentName  string
	NextName     string
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
		"paused":   fc.Paused,
		"current":  fc.CurrentName,
		"next":     fc.NextName,
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
	AddNotification(req.Title, req.Message)
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
	AddNotification(req.Title, req.Message)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Server) APINotificationHistory(c *gin.Context) {
	c.JSON(http.StatusOK, GetNotificationHistory())
}

func (s *Server) AdminNotifications(c *gin.Context) {
	c.HTML(http.StatusOK, "notifications.html", gin.H{
		"notifications": GetNotificationHistory(),
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

func AddNotification(title, message string) {
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

func GetNotificationHistory() []notifEntry {
	priorityMu.Lock()
	defer priorityMu.Unlock()
	out := make([]notifEntry, len(notifHistory))
	copy(out, notifHistory)
	return out
}
