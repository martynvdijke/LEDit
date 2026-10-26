package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ledit/ent"
	"ledit/ent/composition"
	"ledit/ent/pixelart"
	"ledit/ent/playlist"
	"ledit/ent/scene"
	"ledit/ent/textslide"
	"ledit/ent/theme"
)

// ---------------------------------------------------------------------------
// Listing for the share page
// ---------------------------------------------------------------------------

type artifactListRow struct {
	Kind       string
	KindLabel  string
	ID         int
	Name       string
	Detail     string
	Exportable bool
}

func artifactKindLabel(kind string) string {
	switch kind {
	case artifactKindTheme:
		return "Theme"
	case artifactKindLayout:
		return "Layout"
	case artifactKindPlaylist:
		return "Playlist"
	case artifactKindScene:
		return "Scene"
	case artifactKindTextSlide:
		return "Text slide"
	case artifactKindPixelArt:
		return "Pixel art"
	default:
		return kind
	}
}

const textSlideNameLimit = 48

func textSlideDisplayName(content string) string {
	content = strings.TrimSpace(strings.Join(strings.Fields(content), " "))
	if content == "" {
		return "(empty slide)"
	}
	if len(content) > textSlideNameLimit {
		return content[:textSlideNameLimit] + "…"
	}
	return content
}

// (s *Server) artifactListRows collects every shareable artifact of this
// instance. Built-in themes are listed but not exportable.
func (s *Server) artifactListRows() []artifactListRow {
	ctx := s.Ctx
	rows := []artifactListRow{}

	if list, err := s.DB.Theme.Query().Order(ent.Asc(theme.FieldID)).All(ctx); err == nil {
		for _, r := range list {
			rows = append(rows, artifactListRow{
				Kind: artifactKindTheme, KindLabel: artifactKindLabel(artifactKindTheme),
				ID: r.ID, Name: r.Name, Detail: r.BgColor + " / " + r.AccentColor,
				Exportable: !r.BuiltIn,
			})
		}
	}
	if list, err := s.DB.Composition.Query().Order(ent.Asc(composition.FieldID)).All(ctx); err == nil {
		for _, r := range list {
			rows = append(rows, artifactListRow{
				Kind: artifactKindLayout, KindLabel: artifactKindLabel(artifactKindLayout),
				ID: r.ID, Name: r.Name, Detail: fmt.Sprintf("%s %dx%d", r.Mode, r.Rows, r.Cols),
				Exportable: true,
			})
		}
	}
	if list, err := s.DB.Playlist.Query().Order(ent.Asc(playlist.FieldID)).All(ctx); err == nil {
		for _, r := range list {
			rows = append(rows, artifactListRow{
				Kind: artifactKindPlaylist, KindLabel: artifactKindLabel(artifactKindPlaylist),
				ID: r.ID, Name: r.Name, Exportable: true,
			})
		}
	}
	if list, err := s.DB.Scene.Query().Order(ent.Asc(scene.FieldID)).All(ctx); err == nil {
		for _, r := range list {
			rows = append(rows, artifactListRow{
				Kind: artifactKindScene, KindLabel: artifactKindLabel(artifactKindScene),
				ID: r.ID, Name: r.Name, Exportable: true,
			})
		}
	}
	if list, err := s.DB.TextSlide.Query().Order(ent.Asc(textslide.FieldID)).All(ctx); err == nil {
		for _, r := range list {
			rows = append(rows, artifactListRow{
				Kind: artifactKindTextSlide, KindLabel: artifactKindLabel(artifactKindTextSlide),
				ID: r.ID, Name: textSlideDisplayName(r.Content), Exportable: true,
			})
		}
	}
	if list, err := s.DB.PixelArt.Query().Order(ent.Asc(pixelart.FieldID)).All(ctx); err == nil {
		for _, r := range list {
			rows = append(rows, artifactListRow{
				Kind: artifactKindPixelArt, KindLabel: artifactKindLabel(artifactKindPixelArt),
				ID: r.ID, Name: r.Name, Detail: fmt.Sprintf("%dx%d", r.GridWidth, r.GridHeight),
				Exportable: true,
			})
		}
	}
	return rows
}

// ---------------------------------------------------------------------------
// Export
// ---------------------------------------------------------------------------

func artifactDownloadName(kind, name string) string {
	slug := slugify(name)
	if slug == "" {
		slug = "artifact"
	}
	return fmt.Sprintf("ledit-%s-%s.zip", kind, slug)
}

// AdminArtifactExport streams one artifact bundle as a download.
func (s *Server) AdminArtifactExport(c *gin.Context) {
	kind := c.Param("kind")
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		writeArtifactError(c, artifactErr(errCodeInvalidBundle, "invalid artifact id"))
		return
	}
	data, manifest, aerr := s.ExportArtifact(kind, id)
	if aerr != nil {
		writeArtifactError(c, aerr)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", artifactDownloadName(kind, manifest.Name)))
	c.Data(http.StatusOK, "application/zip", data)
}

// ---------------------------------------------------------------------------
// Import
// ---------------------------------------------------------------------------

// readArtifactImportBody extracts the bundle bytes from a multipart file, a raw
// body, or a remote url, honoring the 8 MiB cap before anything else.
func (s *Server) readArtifactImportBody(c *gin.Context) ([]byte, artifactImportOptions, *artifactError) {
	opts := artifactImportOptions{
		Name:       strings.TrimSpace(c.PostForm("name")),
		OnConflict: strings.ToLower(strings.TrimSpace(c.PostForm("on_conflict"))),
		SHA256:     strings.ToLower(strings.TrimSpace(c.PostForm("sha256"))),
		Source:     "upload",
	}
	if opts.Name == "" {
		opts.Name = strings.TrimSpace(c.Query("name"))
	}
	if opts.SHA256 == "" {
		opts.SHA256 = strings.ToLower(strings.TrimSpace(c.Query("sha256")))
	}
	if raw := strings.TrimSpace(c.PostForm("dry_run")); raw == "1" || strings.EqualFold(raw, "true") || strings.EqualFold(raw, "on") {
		opts.DryRun = true
	}

	if file, err := c.FormFile("file"); err == nil && file != nil {
		if file.Size > artifactMaxBodyBytes {
			return nil, opts, artifactErr(errCodeBundleTooLarge, "the bundle exceeds the 8 MiB limit")
		}
		f, err := file.Open()
		if err != nil {
			return nil, opts, artifactErr(errCodeInvalidBundle, "could not read the uploaded bundle")
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, artifactMaxBodyBytes+1))
		if err != nil {
			return nil, opts, artifactErr(errCodeInvalidBundle, "could not read the uploaded bundle")
		}
		if len(data) > artifactMaxBodyBytes {
			return nil, opts, artifactErr(errCodeBundleTooLarge, "the bundle exceeds the 8 MiB limit")
		}
		return data, opts, nil
	}

	if remote := strings.TrimSpace(c.PostForm("url")); remote != "" {
		opts.Source = "url"
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		data, aerr := fetchArtifactURL(ctx, remote, artifactMaxBodyBytes)
		if aerr != nil {
			return nil, opts, aerr
		}
		return data, opts, nil
	}

	data, err := io.ReadAll(io.LimitReader(c.Request.Body, artifactMaxBodyBytes+1))
	if err != nil {
		return nil, opts, artifactErr(errCodeInvalidBundle, "could not read the request body")
	}
	if len(data) == 0 {
		return nil, opts, artifactErr(errCodeInvalidBundle, "no bundle was provided")
	}
	if len(data) > artifactMaxBodyBytes {
		return nil, opts, artifactErr(errCodeBundleTooLarge, "the bundle exceeds the 8 MiB limit")
	}
	return data, opts, nil
}

// AdminArtifactImport is the JSON import API.
func (s *Server) AdminArtifactImport(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, artifactMaxBodyBytes+1024)
	data, opts, aerr := s.readArtifactImportBody(c)
	if aerr != nil {
		writeArtifactError(c, aerr)
		return
	}
	result, aerr := s.importArtifact(c, data, opts)
	if aerr != nil {
		writeArtifactError(c, aerr)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, result)
}

// AdminArtifactImportForm is the HTML wrapper: it imports and redirects back to
// the share page with a flash message.
func (s *Server) AdminArtifactImportForm(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, artifactMaxBodyBytes+1024)
	data, opts, aerr := s.readArtifactImportBody(c)
	if aerr != nil {
		SetFlash(c, "danger", aerr.Message)
		c.Redirect(http.StatusFound, "/admin/artifacts")
		return
	}
	result, aerr := s.importArtifact(c, data, opts)
	if aerr != nil {
		SetFlash(c, "danger", aerr.Message)
		c.Redirect(http.StatusFound, "/admin/artifacts")
		return
	}
	switch {
	case result.Skipped:
		SetFlash(c, "info", fmt.Sprintf("Skipped %s %q (already present).", result.Kind, result.Name))
	case result.Replaced:
		SetFlash(c, "success", fmt.Sprintf("Replaced %s %q.", result.Kind, result.Name))
	default:
		SetFlash(c, "success", fmt.Sprintf("Imported %s %q.", result.Kind, result.Name))
	}
	c.Redirect(http.StatusFound, "/admin/artifacts")
}

// ---------------------------------------------------------------------------
// Remote fetch (SSRF hardened)
// ---------------------------------------------------------------------------

func artifactAllowlistHosts() []string {
	raw := strings.TrimSpace(os.Getenv("ARTIFACT_CATALOG_HOSTS"))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	hosts := make([]string, 0, len(parts))
	for _, p := range parts {
		if h := strings.ToLower(strings.TrimSpace(p)); h != "" {
			hosts = append(hosts, h)
		}
	}
	return hosts
}

func artifactPublicAddr(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	return true
}

// artifactHostAllowed enforces the host policy for remote artifact fetches: an
// explicit allowlist when configured, otherwise public addresses only.
func artifactHostAllowed(ctx context.Context, host string) (bool, *artifactError) {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false, artifactErr(errCodeInvalidURL, "the URL host is empty")
	}
	if allow := artifactAllowlistHosts(); len(allow) > 0 {
		for _, h := range allow {
			if h == host {
				return true, nil
			}
		}
		return false, artifactErr(errCodeHostNotAllowed, "the URL host is not in the ArtifactCatalogHosts allowlist")
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(addrs) == 0 {
		return false, artifactErr(errCodeFetchFailed, "the URL host could not be resolved")
	}
	for _, addr := range addrs {
		if !artifactPublicAddr(addr.IP) {
			return false, artifactErr(errCodeHostNotAllowed, "the URL host resolves to a non-public address")
		}
	}
	return true, nil
}

var artifactHTTPClient = &http.Client{
	Timeout: 10 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// fetchArtifactURL downloads a bundle under the remote-fetch policy in D8.
func fetchArtifactURL(ctx context.Context, rawURL string, maxBytes int64) ([]byte, *artifactError) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, artifactErr(errCodeInvalidURL, "the bundle URL is not valid")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, artifactErr(errCodeInvalidURL, "only http and https URLs are supported")
	}
	if ok, aerr := artifactHostAllowed(ctx, u.Hostname()); !ok {
		return nil, aerr
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, artifactErr(errCodeInvalidURL, "the bundle URL is not valid")
	}
	resp, err := artifactHTTPClient.Do(req)
	if err != nil {
		return nil, artifactErr(errCodeFetchFailed, "the bundle could not be downloaded")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, artifactErr(errCodeFetchFailed, fmt.Sprintf("the bundle URL returned status %d", resp.StatusCode))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, artifactErr(errCodeFetchFailed, "the bundle could not be downloaded")
	}
	if int64(len(data)) > maxBytes {
		return nil, artifactErr(errCodeBundleTooLarge, "the remote bundle exceeds the size limit")
	}
	return data, nil
}

// ---------------------------------------------------------------------------
// Catalog
// ---------------------------------------------------------------------------

// artifactCatalogEntry is one row of the remote catalog index.
type artifactCatalogEntry struct {
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Author      string `json:"author"`
	Description string `json:"description"`
	BundleURL   string `json:"bundle_url"`
	PreviewURL  string `json:"preview_url"`
	SHA256      string `json:"sha256"`
	Size        int64  `json:"size"`
	CreatedWith string `json:"created_with"`
}

type artifactCatalogIndex struct {
	Artifacts []artifactCatalogEntry `json:"artifacts"`
}

// fetchArtifactCatalog downloads and parses the configured catalog index.
func fetchArtifactCatalog(ctx context.Context) ([]artifactCatalogEntry, string, *artifactError) {
	raw := strings.TrimSpace(os.Getenv("ARTIFACT_CATALOG_URL"))
	if raw == "" {
		return nil, "", nil
	}
	data, aerr := fetchArtifactURL(ctx, raw, artifactCatalogMaxBytes)
	if aerr != nil {
		return nil, raw, aerr
	}
	var idx artifactCatalogIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, raw, artifactErr(errCodeInvalidJSON, "the catalog index is not valid JSON")
	}
	return idx.Artifacts, raw, nil
}

// AdminArtifactsPage renders the share/import page. Query parameters only
// prefill the form; nothing is fetched until the admin confirms.
func (s *Server) AdminArtifactsPage(c *gin.Context) {
	vars := gin.H{
		"active":   "artifacts",
		"rows":     s.artifactListRows(),
		"catalog":  strings.TrimSpace(os.Getenv("ARTIFACT_CATALOG_URL")) != "",
		"prefill":  gin.H{},
		"deepLink": gin.H{},
	}
	prefill := gin.H{}
	for _, key := range []string{"url", "sha256", "name", "on_conflict"} {
		if v := strings.TrimSpace(c.Query(key)); v != "" {
			prefill[key] = v
		}
	}
	vars["prefill"] = prefill
	s.renderPage(c, http.StatusOK, "artifacts.html", vars)
}

// AdminArtifactCatalog renders the remote catalog, if configured.
func (s *Server) AdminArtifactCatalog(c *gin.Context) {
	raw := strings.TrimSpace(os.Getenv("ARTIFACT_CATALOG_URL"))
	if raw == "" {
		s.renderPage(c, http.StatusOK, "artifact_catalog.html", gin.H{"active": "artifact_catalog", "catalogURL": "", "entries": []artifactCatalogEntry{}})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	entries, _, aerr := fetchArtifactCatalog(ctx)
	vars := gin.H{"active": "artifact_catalog", "catalogURL": raw, "entries": entries}
	if aerr != nil {
		vars["error"] = aerr.Message
	}
	s.renderPage(c, http.StatusOK, "artifact_catalog.html", vars)
}

// AdminArtifactCatalogInstall fetches one catalog bundle and installs it
// through the shared validation/conflict/transaction path.
func (s *Server) AdminArtifactCatalogInstall(c *gin.Context) {
	bundleURL := strings.TrimSpace(c.PostForm("bundle_url"))
	if bundleURL == "" {
		SetFlash(c, "danger", "No bundle URL was provided.")
		c.Redirect(http.StatusFound, "/admin/artifacts/catalog")
		return
	}
	wantSHA := strings.ToLower(strings.TrimSpace(c.PostForm("sha256")))
	wantSize, _ := strconv.ParseInt(strings.TrimSpace(c.PostForm("size")), 10, 64)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	data, aerr := fetchArtifactURL(ctx, bundleURL, artifactMaxBodyBytes)
	if aerr != nil {
		SetFlash(c, "danger", aerr.Message)
		c.Redirect(http.StatusFound, "/admin/artifacts/catalog")
		return
	}
	if wantSHA != "" {
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != wantSHA {
			SetFlash(c, "danger", "The downloaded bundle did not match its catalog checksum.")
			c.Redirect(http.StatusFound, "/admin/artifacts/catalog")
			return
		}
	}
	if wantSize > 0 && int64(len(data)) != wantSize {
		SetFlash(c, "danger", "The downloaded bundle did not match its catalog size.")
		c.Redirect(http.StatusFound, "/admin/artifacts/catalog")
		return
	}
	opts := artifactImportOptions{
		Name:       strings.TrimSpace(c.PostForm("name")),
		OnConflict: strings.ToLower(strings.TrimSpace(c.PostForm("on_conflict"))),
		SHA256:     wantSHA,
		Source:     "catalog",
	}
	result, aerr := s.importArtifact(c, data, opts)
	if aerr != nil {
		SetFlash(c, "danger", aerr.Message)
		c.Redirect(http.StatusFound, "/admin/artifacts/catalog")
		return
	}
	SetFlash(c, "success", fmt.Sprintf("Installed %s %q.", result.Kind, result.Name))
	c.Redirect(http.StatusFound, "/admin/artifacts")
}
