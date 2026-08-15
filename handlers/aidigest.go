package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"ledit/datasource"
	"ledit/ent"
	"ledit/ent/generalsettings"
)

// defaultDigestTTLMinutes is applied when the form field is missing/invalid.
const defaultDigestTTLMinutes = 30

func (s *Server) digestFeedOptions(c *gin.Context) ([]*ent.RssFeed, []*ent.NewsFeed) {
	settings, err := s.DB.GeneralSettings.Query().
		Where(generalsettings.ID(1)).
		WithRssFeeds().WithNewsFeeds().
		Only(c.Request.Context())
	if err != nil {
		return nil, nil
	}
	rss, _ := settings.Edges.RssFeedsOrErr()
	news, _ := settings.Edges.NewsFeedsOrErr()
	return rss, news
}

func (s *Server) AdminAIDigestNew(c *gin.Context) {
	rss, news := s.digestFeedOptions(c)
	s.renderPage(c, http.StatusOK, "aidigest_form.html", gin.H{
		"rssFeeds":  rss,
		"newsFeeds": news,
	})
}

func (s *Server) AdminAIDigestCreate(c *gin.Context) {
	name := c.PostForm("name")
	prompt := c.PostForm("prompt")
	sources := marshalDigestSources(c.PostFormArray("sources"))
	ttl := digestTTLMinutes(c.PostForm("ttl_minutes"))
	enabled := c.PostForm("enabled") == "on"

	v := NewValidator().Required("Name", name)
	if !v.Valid() {
		SetFlash(c, "danger", v.Error())
		c.Redirect(http.StatusFound, "/admin/aidigests/new")
		return
	}

	obj := s.DB.AIDigest.Create().
		SetName(name).
		SetPrompt(prompt).
		SetSources(sources).
		SetTTLMinutes(ttl).
		SetEnabled(enabled).
		SaveX(s.Ctx)
	if settings, err := s.DB.GeneralSettings.Query().Where(generalsettings.ID(1)).Only(s.Ctx); err == nil {
		s.DB.GeneralSettings.UpdateOne(settings).AddAiDigests(obj).Exec(s.Ctx)
	}
	SetFlash(c, "success", "AI Digest created")
	c.Redirect(http.StatusFound, "/admin/")
}

func (s *Server) AdminAIDigestEdit(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	obj, err := s.DB.AIDigest.Get(s.Ctx, id)
	if err != nil {
		SetFlash(c, "danger", "AI Digest not found")
		c.Redirect(http.StatusFound, "/admin/")
		return
	}
	rss, news := s.digestFeedOptions(c)
	s.renderPage(c, http.StatusOK, "aidigest_form.html", gin.H{
		"obj":       obj,
		"edit":      true,
		"selected":  datasource.ParseDigestSources(obj.Sources),
		"rssFeeds":  rss,
		"newsFeeds": news,
	})
}

func (s *Server) AdminAIDigestUpdate(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	name := c.PostForm("name")
	prompt := c.PostForm("prompt")
	sources := marshalDigestSources(c.PostFormArray("sources"))
	ttl := digestTTLMinutes(c.PostForm("ttl_minutes"))
	enabled := c.PostForm("enabled") == "on"

	v := NewValidator().Required("Name", name)
	if !v.Valid() {
		SetFlash(c, "danger", v.Error())
		c.Redirect(http.StatusFound, "/admin/aidigests/"+strconv.Itoa(id)+"/edit")
		return
	}

	s.DB.AIDigest.UpdateOneID(id).
		SetName(name).
		SetPrompt(prompt).
		SetSources(sources).
		SetTTLMinutes(ttl).
		SetEnabled(enabled).
		Exec(s.Ctx)
	datasource.InvalidateDigest(id)
	SetFlash(c, "success", "AI Digest updated")
	c.Redirect(http.StatusFound, "/admin/")
}

func (s *Server) AdminAIDigestDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	s.DB.AIDigest.DeleteOneID(id).Exec(s.Ctx)
	datasource.InvalidateDigest(id)
	SetFlash(c, "success", "AI Digest deleted")
	c.Redirect(http.StatusFound, "/admin/")
}

func (s *Server) AdminAIDigestRefresh(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	datasource.InvalidateDigest(id)
	SetFlash(c, "success", "AI Digest refreshed")
	c.Redirect(http.StatusFound, "/admin/aidigests/"+strconv.Itoa(id)+"/edit")
}

// marshalDigestSources encodes a list of selected feed names as a JSON array.
func marshalDigestSources(names []string) string {
	b, err := json.Marshal(names)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// digestTTLMinutes parses and clamps the ttl_minutes form value (>= 1).
func digestTTLMinutes(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return defaultDigestTTLMinutes
	}
	return n
}
