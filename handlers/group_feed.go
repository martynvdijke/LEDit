package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"ledit/ent/devicesettings"
)

type groupFeedResult struct {
	Total   int      `json:"total"`
	Sent    int      `json:"sent"`
	Offline int      `json:"offline"`
	Errors  []string `json:"errors"`
}

func (s *Server) groupFeedAction(c *gin.Context, action string) {
	gid, _ := strconv.Atoi(c.Param("id"))
	group, err := s.DB.DeviceGroup.Get(s.Ctx, gid)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "group not found"})
		return
	}
	_ = group
	members, _ := s.DB.DeviceSettings.Query().Where(devicesettings.GroupIDEQ(gid)).All(s.Ctx)
	total := len(members)
	sent := 0
	offline := 0
	var errs []string
	// For priority, need title/message
	var pTitle, pMsg string
	if action == "priority" {
		var req priorityMsg
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		pTitle = req.Title
		pMsg = req.Message
	}
	for _, m := range members {
		fc, ok := getDeviceFeed(m.ID)
		if !ok {
			offline++
			continue
		}
		switch action {
		case "pause":
			fc.Pause()
		case "resume":
			fc.Resume()
		case "next":
			fc.Next()
		case "priority":
			s.AddNotification(pTitle, pMsg)
			_ = fc
		}
		sent++
	}
	if errs == nil {
		errs = []string{}
	}
	c.JSON(http.StatusOK, groupFeedResult{Total: total, Sent: sent, Offline: offline, Errors: errs})
}

func (s *Server) APIGroupFeedPause(c *gin.Context)    { s.groupFeedAction(c, "pause") }
func (s *Server) APIGroupFeedResume(c *gin.Context)   { s.groupFeedAction(c, "resume") }
func (s *Server) APIGroupFeedNext(c *gin.Context)     { s.groupFeedAction(c, "next") }
func (s *Server) APIGroupFeedPriority(c *gin.Context) { s.groupFeedAction(c, "priority") }
