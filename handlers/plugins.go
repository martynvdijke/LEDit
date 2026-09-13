package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"ledit/datasource"
	"ledit/ent/datasourceplugin"
)

func formCheckbox(c *gin.Context, name string) bool {
	v := c.PostForm(name)
	return v == "on" || v == "true" || v == "1"
}

// parsePluginForm reads manifest + config from an admin plugin form. Manifest
// field inputs are named cfg_<key>; a raw `config` JSON textarea wins when set.
func parsePluginForm(c *gin.Context) (manifestRaw string, manifest *datasource.PluginManifest, config json.RawMessage, err error) {
	manifestRaw = strings.TrimSpace(c.PostForm("manifest"))
	manifest, err = datasource.ParsePluginManifest(manifestRaw)
	if err != nil {
		return
	}
	if raw := strings.TrimSpace(c.PostForm("config")); raw != "" {
		var obj map[string]any
		if e := json.Unmarshal([]byte(raw), &obj); e != nil {
			err = fmt.Errorf("config must be a JSON object: %w", e)
			return
		}
		config = json.RawMessage(raw)
	} else {
		obj := map[string]any{}
		if manifest != nil {
			for _, f := range manifest.Fields {
				key := "cfg_" + f.Key
				switch f.Type {
				case "bool":
					obj[f.Key] = formCheckbox(c, key)
				case "number":
					s := strings.TrimSpace(c.PostForm(key))
					if s == "" {
						continue
					}
					n, e := strconv.ParseFloat(s, 64)
					if e != nil {
						err = fmt.Errorf("config field %q must be a number", f.Key)
						return
					}
					obj[f.Key] = n
				default:
					s := c.PostForm(key)
					if s == "" {
						continue
					}
					obj[f.Key] = s
				}
			}
		}
		b, _ := json.Marshal(obj)
		config = b
	}
	err = datasource.ValidatePluginConfig(manifest, config)
	return
}

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
	manifestRaw, _, config, err := parsePluginForm(c)
	if err != nil {
		SetFlash(c, "danger", err.Error())
		c.Redirect(http.StatusFound, "/admin/plugins/new")
		return
	}
	_, err = s.DB.DatasourcePlugin.Create().SetName(name).SetKind(datasourceplugin.Kind(kind)).SetTarget(target).SetEnabled(enabled).SetTimeoutMs(timeout).SetManifest(manifestRaw).SetConfig(string(config)).Save(s.Ctx)
	if err != nil {
		SetFlash(c, "danger", "Failed to create: "+err.Error())
		c.Redirect(http.StatusFound, "/admin/plugins/new")
		return
	}
	reloadPluginCache(s.DB)
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
	manifestRaw, _, config, err := parsePluginForm(c)
	if err != nil {
		SetFlash(c, "danger", err.Error())
		c.Redirect(http.StatusFound, "/admin/plugins/"+c.Param("id")+"/edit")
		return
	}
	err = s.DB.DatasourcePlugin.UpdateOneID(id).SetName(name).SetKind(datasourceplugin.Kind(kind)).SetTarget(target).SetEnabled(enabled).SetTimeoutMs(timeout).SetManifest(manifestRaw).SetConfig(string(config)).Exec(s.Ctx)
	if err != nil {
		SetFlash(c, "danger", "Failed to update: "+err.Error())
	}
	reloadPluginCache(s.DB)
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
		Name      string          `json:"name"`
		Kind      string          `json:"kind"`
		Target    string          `json:"target"`
		Enabled   bool            `json:"enabled"`
		TimeoutMs int             `json:"timeout_ms"`
		Manifest  string          `json:"manifest"`
		Config    json.RawMessage `json:"config"`
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
	m, err := datasource.ParsePluginManifest(req.Manifest)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	cfg := req.Config
	if len(cfg) == 0 {
		cfg = json.RawMessage(`{}`)
	}
	if err := datasource.ValidatePluginConfig(m, cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	p, err := s.DB.DatasourcePlugin.Create().SetName(req.Name).SetKind(datasourceplugin.Kind(req.Kind)).SetTarget(req.Target).SetEnabled(req.Enabled).SetTimeoutMs(req.TimeoutMs).SetManifest(strings.TrimSpace(req.Manifest)).SetConfig(string(cfg)).Save(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	reloadPluginCache(s.DB)
	c.JSON(http.StatusCreated, p)
}

func (s *Server) APIPluginUpdate(c *gin.Context) {
	if !s.IsAuthenticated(c) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	var req struct {
		Name      string          `json:"name"`
		Kind      string          `json:"kind"`
		Target    string          `json:"target"`
		Enabled   bool            `json:"enabled"`
		TimeoutMs int             `json:"timeout_ms"`
		Manifest  string          `json:"manifest"`
		Config    json.RawMessage `json:"config"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validatePluginTarget(req.Kind, req.Target); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	m, err := datasource.ParsePluginManifest(req.Manifest)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	upd := s.DB.DatasourcePlugin.UpdateOneID(id).SetName(req.Name).SetKind(datasourceplugin.Kind(req.Kind)).SetTarget(req.Target).SetEnabled(req.Enabled).SetTimeoutMs(req.TimeoutMs)
	if req.Manifest != "" || req.Config != nil {
		cfg := req.Config
		if len(cfg) == 0 {
			cfg = json.RawMessage(`{}`)
		}
		if err := datasource.ValidatePluginConfig(m, cfg); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		upd = upd.SetManifest(strings.TrimSpace(req.Manifest)).SetConfig(string(cfg))
	}
	if err := upd.Exec(c.Request.Context()); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	reloadPluginCache(s.DB)
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

// --- Install from manifest URL / optional catalog ---

const pluginManifestMaxBytes = 64 * 1024

func manifestHostAllowed(host string) bool {
	allow := strings.TrimSpace(os.Getenv("PLUGINS_MANIFEST_HOSTS"))
	if allow == "" {
		return true
	}
	for _, h := range strings.Split(allow, ",") {
		if strings.EqualFold(strings.TrimSpace(h), host) {
			return true
		}
	}
	return false
}

// installPluginFromURL fetches a manifest and creates a disabled plugin from it.
func (s *Server) installPluginFromURL(ctx context.Context, rawURL string) (int, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return 0, fmt.Errorf("url must be http or https")
	}
	if !manifestHostAllowed(u.Host) {
		return 0, fmt.Errorf("manifest host %s not allowed", u.Host)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("manifest fetch: http %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, pluginManifestMaxBytes))
	if err != nil {
		return 0, err
	}
	m, err := datasource.ParsePluginManifest(string(body))
	if err != nil {
		return 0, err
	}
	if m == nil || strings.TrimSpace(m.Name) == "" {
		return 0, fmt.Errorf("manifest missing name")
	}
	kind := m.Kind
	if kind == "" {
		kind = "exec"
	}
	if kind != "exec" && kind != "http" {
		return 0, fmt.Errorf("manifest kind must be exec or http")
	}
	if err := validatePluginTarget(kind, m.Target); err != nil {
		return 0, err
	}
	obj := map[string]any{}
	for _, f := range m.Fields {
		if f.Default != "" {
			obj[f.Key] = f.Default
		}
	}
	cfg, _ := json.Marshal(obj)
	if err := datasource.ValidatePluginConfig(m, cfg); err != nil {
		return 0, err
	}
	timeout := m.TimeoutMs
	if timeout == 0 {
		timeout = 3000
	}
	p, err := s.DB.DatasourcePlugin.Create().SetName(m.Name).SetKind(datasourceplugin.Kind(kind)).SetTarget(m.Target).SetEnabled(false).SetTimeoutMs(timeout).SetManifest(strings.TrimSpace(string(body))).SetConfig(string(cfg)).Save(ctx)
	if err != nil {
		return 0, err
	}
	reloadPluginCache(s.DB)
	return p.ID, nil
}

// AdminPluginInstall installs a plugin from a manifest URL.
func (s *Server) AdminPluginInstall(c *gin.Context) {
	if _, err := s.installPluginFromURL(c.Request.Context(), c.PostForm("url")); err != nil {
		SetFlash(c, "danger", "Install failed: "+err.Error())
		c.Redirect(http.StatusFound, "/admin/plugins")
		return
	}
	SetFlash(c, "success", "Plugin installed (disabled) — review and enable it")
	c.Redirect(http.StatusFound, "/admin/plugins")
}

type pluginCatalogEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	URL         string `json:"url"`
}

// AdminPluginCatalog lists plugins from PLUGINS_CATALOG_URL, if configured.
func (s *Server) AdminPluginCatalog(c *gin.Context) {
	raw := strings.TrimSpace(os.Getenv("PLUGINS_CATALOG_URL"))
	var entries []pluginCatalogEntry
	var errMsg string
	if raw != "" {
		var err error
		entries, err = fetchPluginCatalog(c.Request.Context(), raw)
		if err != nil {
			errMsg = err.Error()
		}
	}
	c.HTML(http.StatusOK, "plugin_catalog.html", gin.H{"catalogURL": raw, "entries": entries, "error": errMsg})
}

func fetchPluginCatalog(ctx context.Context, rawURL string) ([]pluginCatalogEntry, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("catalog url must be http or https")
	}
	if !manifestHostAllowed(u.Host) {
		return nil, fmt.Errorf("catalog host %s not allowed", u.Host)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("catalog fetch: http %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return nil, err
	}
	var doc struct {
		Plugins []pluginCatalogEntry `json:"plugins"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("catalog must be JSON {plugins:[...]}: %w", err)
	}
	return doc.Plugins, nil
}
