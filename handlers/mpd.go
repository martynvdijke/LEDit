package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func (s *Server) handleMPDCreate(c *gin.Context) {
	host := c.PostForm("host")
	portStr := c.PostForm("port")
	password := c.PostForm("password")
	if host == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "host required"})
		return
	}
	port, _ := strconv.Atoi(portStr)
	if port < 1 || port > 65535 {
		port = 6600
	}
	_, err := s.DB.MPD.Create().SetHost(host).SetPort(port).SetPassword(password).SetEnabled(true).Save(s.Ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Redirect(http.StatusFound, "/admin/")
}

func (s *Server) handleMPDUpdate(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	host := c.PostForm("host")
	portStr := c.PostForm("port")
	password := c.PostForm("password")
	port, _ := strconv.Atoi(portStr)
	if host == "" || port < 1 || port > 65535 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid host/port"})
		return
	}
	if err := s.DB.MPD.UpdateOneID(id).SetHost(host).SetPort(port).SetPassword(password).Exec(s.Ctx); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Redirect(http.StatusFound, "/admin/")
}

func (s *Server) handleProviderUpdate(c *gin.Context) {
	provider := c.PostForm("now_playing_provider")
	if provider != "jellyfin" && provider != "mpd" && provider != "disabled" {
		provider = "disabled"
	}
	_ = s.DB.GeneralSettings.UpdateOneID(1).SetNowPlayingProvider(provider).Exec(s.Ctx)
	c.Redirect(http.StatusFound, "/admin/")
}
