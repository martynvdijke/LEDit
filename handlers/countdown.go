package handlers

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"ledit/ent/generalsettings"
)

// countdownTimeLayout is the value of <input type="datetime-local">.
const countdownTimeLayout = "2006-01-02T15:04"

func parseCountdownTime(s string) (time.Time, error) {
	return time.ParseInLocation(countdownTimeLayout, s, time.Local)
}

func (s *Server) AdminCountdownNew(c *gin.Context) {
	s.renderPage(c, http.StatusOK, "countdown_form.html", gin.H{})
}

func (s *Server) AdminCountdownCreate(c *gin.Context) {
	name := c.PostForm("name")
	targetStr := c.PostForm("target_time")
	label := c.PostForm("label")
	enabled := c.PostForm("enabled") == "on"

	v := NewValidator().Required("Name", name)
	if v.Valid() {
		if _, err := parseCountdownTime(targetStr); err != nil {
			SetFlash(c, "danger", "Target time is required")
			c.Redirect(http.StatusFound, "/admin/countdowns/new")
			return
		}
	}
	if !v.Valid() {
		SetFlash(c, "danger", v.Error())
		c.Redirect(http.StatusFound, "/admin/countdowns/new")
		return
	}

	target, _ := parseCountdownTime(targetStr)
	obj := s.DB.Countdown.Create().SetName(name).SetTargetTime(target).SetLabel(label).SetEnabled(enabled).SaveX(s.Ctx)
	if settings, err := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(s.Ctx); err == nil {
		s.DB.GeneralSettings.UpdateOne(settings).AddCountdowns(obj).Exec(s.Ctx)
	}
	SetFlash(c, "success", "Countdown created")
	c.Redirect(http.StatusFound, "/admin/")
}

func (s *Server) AdminCountdownEdit(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	obj, err := s.DB.Countdown.Get(s.Ctx, id)
	if err != nil {
		SetFlash(c, "danger", "Countdown not found")
		c.Redirect(http.StatusFound, "/admin/")
		return
	}
	s.renderPage(c, http.StatusOK, "countdown_form.html", gin.H{
		"obj":  obj,
		"edit": true,
	})
}

func (s *Server) AdminCountdownUpdate(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	name := c.PostForm("name")
	targetStr := c.PostForm("target_time")
	label := c.PostForm("label")
	enabled := c.PostForm("enabled") == "on"

	v := NewValidator().Required("Name", name)
	target, err := parseCountdownTime(targetStr)
	if !v.Valid() || err != nil {
		SetFlash(c, "danger", "Name and target time are required")
		c.Redirect(http.StatusFound, "/admin/countdowns/"+strconv.Itoa(id)+"/edit")
		return
	}

	s.DB.Countdown.UpdateOneID(id).SetName(name).SetTargetTime(target).SetLabel(label).SetEnabled(enabled).Exec(s.Ctx)
	SetFlash(c, "success", "Countdown updated")
	c.Redirect(http.StatusFound, "/admin/")
}

func (s *Server) AdminCountdownDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	s.DB.Countdown.DeleteOneID(id).Exec(s.Ctx)
	SetFlash(c, "success", "Countdown deleted")
	c.Redirect(http.StatusFound, "/admin/")
}
