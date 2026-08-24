package handlers

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"ledit/datasource"
	"ledit/ent/datasourceplugin"
)

func validatePluginTarget(kind, target string) error {
	if kind == "exec" {
		if target == "" {
			return fmt.Errorf("target required")
		}
		prefix := os.Getenv("PLUGINS_ALLOW_PREFIX")
		if prefix != "" && !strings.HasPrefix(filepath.Clean(target), filepath.Clean(prefix)) {
			return fmt.Errorf("exec target must be under PLUGINS_ALLOW_PREFIX=%s", prefix)
		}
		fi, err := os.Stat(target)
		if err != nil {
			return fmt.Errorf("exec target not found: %v", err)
		}
		if fi.IsDir() {
			return fmt.Errorf("exec target is a directory")
		}
		if fi.Mode().Perm()&0111 == 0 {
			return fmt.Errorf("exec target not executable")
		}
	} else if kind == "http" {
		if err := datasource.ValidateHTTPTarget(target); err != nil {
			return err
		}
	}
	return nil
}

// AdminPlugins list
func (s *Server) AdminPlugins(c *gin.Context) {
	plugins, _ := s.DB.DatasourcePlugin.Query().All(s.Ctx)
	c.HTML(http.StatusOK, "plugins.html", gin.H{"plugins": plugins})
}

func (s *Server) AdminPluginNew(c *gin.Context) {
	prefix := os.Getenv("PLUGINS_ALLOW_PREFIX")
	c.HTML(http.StatusOK, "plugin_form.html", gin.H{"allowPrefix": prefix})
}

func (s *Server) AdminPluginCreate(c *gin.Context) {
	name := strings.TrimSpace(c.PostForm("name"))
	kind := c.PostForm("kind")
	target := strings.TrimSpace(c.PostForm("target"))
	timeout, _ := strconv.Atoi(c.PostForm("timeout_ms"))
	if timeout == 0 {
		timeout = 3000
	}
	enabled := c.PostForm("enabled") == "on" || c.PostForm("enabled") == "true" || c.PostForm("enabled") == "1"
	if name == "" || (kind != "exec" && kind != "http") {
		SetFlash(c, "danger", "name and valid kind required")
		c.Redirect(http.StatusFound, "/admin/plugins/new")
		return
	}
	if err := validatePluginTarget(kind, target); err != nil {
		SetFlash(c, "danger", err.Error())
		c.Redirect(http.StatusFound, "/admin/plugins/new")
		return
	}
	_, err := s.DB.DatasourcePlugin.Create().SetName(name).SetKind(datasourceplugin.Kind(kind)).SetTarget(target).SetEnabled(enabled).SetTimeoutMs(timeout).Save(s.Ctx)
	if err != nil {
		SetFlash(c, "danger", "Failed to create: "+err.Error())
		c.Redirect(http.StatusFound, "/admin/plugins/new")
		return
	}
	SetFlash(c, "success", "Plugin created")
	c.Redirect(http.StatusFound, "/admin/plugins")
}

func (s *Server) AdminPluginEdit(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	p, err := s.DB.DatasourcePlugin.Get(s.Ctx, id)
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/plugins")
		return
	}
	prefix := os.Getenv("PLUGINS_ALLOW_PREFIX")
	health := datasource.GetPluginHealth(id)
	c.HTML(http.StatusOK, "plugin_form.html", gin.H{"plugin": p, "edit": true, "allowPrefix": prefix, "health": health})
}

func (s *Server) AdminPluginUpdate(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	name := strings.TrimSpace(c.PostForm("name"))
	kind := c.PostForm("kind")
	target := strings.TrimSpace(c.PostForm("target"))
	timeout, _ := strconv.Atoi(c.PostForm("timeout_ms"))
	if timeout == 0 {
		timeout = 3000
	}
	enabled := c.PostForm("enabled") == "on" || c.PostForm("enabled") == "true" || c.PostForm("enabled") == "1"
	if err := validatePluginTarget(kind, target); err != nil {
		SetFlash(c, "danger", err.Error())
		c.Redirect(http.StatusFound, "/admin/plugins/"+c.Param("id")+"/edit")
		return
	}
	err := s.DB.DatasourcePlugin.UpdateOneID(id).SetName(name).SetKind(datasourceplugin.Kind(kind)).SetTarget(target).SetEnabled(enabled).SetTimeoutMs(timeout).Exec(s.Ctx)
	if err != nil {
		SetFlash(c, "danger", "Failed to update: "+err.Error())
	}
	c.Redirect(http.StatusFound, "/admin/plugins")
}

func (s *Server) AdminPluginDelete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	_ = s.DB.DatasourcePlugin.DeleteOneID(id).Exec(s.Ctx)
	c.Redirect(http.StatusFound, "/admin/plugins")
}

// API CRUD (session auth, admin)
func (s *Server) APIPluginList(c *gin.Context) {
	if !s.IsAuthenticated(c) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	plugins, _ := s.DB.DatasourcePlugin.Query().All(c.Request.Context())
	c.JSON(http.StatusOK, plugins)
}

func (s *Server) APIPluginGet(c *gin.Context) {
	if !s.IsAuthenticated(c) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	p, err := s.DB.DatasourcePlugin.Get(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, p)
}

func (s *Server) APIPluginCreate(c *gin.Context) {
	if !s.IsAuthenticated(c) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req struct {
		Name      string `json:"name"`
		Kind      string `json:"kind"`
		Target    string `json:"target"`
		Enabled   bool   `json:"enabled"`
		TimeoutMs int    `json:"timeout_ms"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.TimeoutMs == 0 {
		req.TimeoutMs = 3000
	}
	if err := validatePluginTarget(req.Kind, req.Target); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	p, err := s.DB.DatasourcePlugin.Create().SetName(req.Name).SetKind(datasourceplugin.Kind(req.Kind)).SetTarget(req.Target).SetEnabled(req.Enabled).SetTimeoutMs(req.TimeoutMs).Save(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, p)
}

func (s *Server) APIPluginUpdate(c *gin.Context) {
	if !s.IsAuthenticated(c) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	var req struct {
		Name      string `json:"name"`
		Kind      string `json:"kind"`
		Target    string `json:"target"`
		Enabled   bool   `json:"enabled"`
		TimeoutMs int    `json:"timeout_ms"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validatePluginTarget(req.Kind, req.Target); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := s.DB.DatasourcePlugin.UpdateOneID(id).SetName(req.Name).SetKind(datasourceplugin.Kind(req.Kind)).SetTarget(req.Target).SetEnabled(req.Enabled).SetTimeoutMs(req.TimeoutMs).Exec(c.Request.Context()); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	p, _ := s.DB.DatasourcePlugin.Get(c.Request.Context(), id)
	c.JSON(http.StatusOK, p)
}

func (s *Server) APIPluginDelete(c *gin.Context) {
	if !s.IsAuthenticated(c) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	_ = s.DB.DatasourcePlugin.DeleteOneID(id).Exec(c.Request.Context())
	c.JSON(http.StatusOK, gin.H{"deleted": id})
}

func (s *Server) APIPluginHealth(c *gin.Context) {
	if !s.IsAuthenticated(c) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	p, err := s.DB.DatasourcePlugin.Get(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	h := datasource.GetPluginHealth(id)
	if h == nil {
		c.JSON(http.StatusOK, gin.H{"enabled": p.Enabled, "last_latency_ms": nil, "last_exit_code": nil, "last_error": "", "stderr_tail": "", "last_invoked_at": nil})
		return
	}
	c.JSON(http.StatusOK, h)
}
