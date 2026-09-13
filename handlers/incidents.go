package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"ledit/ent"
	"ledit/ent/incident"
	"ledit/render"
)

// Incident TTL bounds and default. A lost resolve webhook must not pin the wall
// forever, so every incident expires; healthy sources re-fire and re-arm it.
const (
	incidentDefaultTTL = 30 * time.Minute
	incidentMinTTL     = 60 * time.Second
	incidentMaxTTL     = 24 * time.Hour
)

// incidentCache holds the active incidents in display order (highest severity
// first, most recent first). It mirrors the alarm-manager pattern: refreshed on
// every write and at startup, pruned by expiry on read, no background job.
var incidentCache struct {
	mu     sync.RWMutex
	active []*ent.Incident
}

// severityRank orders severities for display: lower is more severe.
func severityRank(severity string) int {
	switch severity {
	case "critical":
		return 0
	case "warning":
		return 1
	case "info":
		return 2
	default:
		return 1
	}
}

// StartIncidentManager loads persisted incidents into the in-memory cache.
// Called once from Server.New.
func StartIncidentManager(client *ent.Client) {
	reloadIncidents(client)
}

// reloadIncidents rebuilds the active cache from the database, sorted by
// severity then recency. Expired incidents are excluded.
func reloadIncidents(client *ent.Client) {
	if client == nil {
		return
	}
	rows, err := client.Incident.Query().
		Where(incident.ActiveEQ(true), incident.ExpiresAtGT(time.Now())).
		All(context.Background())
	if err != nil {
		slog.Warn("failed to load incidents", "source", "incident", "error", err)
		return
	}
	sort.SliceStable(rows, func(i, j int) bool {
		ri, rj := severityRank(rows[i].Severity), severityRank(rows[j].Severity)
		if ri != rj {
			return ri < rj
		}
		return rows[i].CreatedAt.After(rows[j].CreatedAt)
	})
	incidentCache.mu.Lock()
	incidentCache.active = rows
	incidentCache.mu.Unlock()
}

// ActiveIncidents returns a copy of the active unexpired incidents in display
// order. Expiry is applied on read, so an expired incident needs no writer.
func ActiveIncidents() []*ent.Incident {
	incidentCache.mu.RLock()
	defer incidentCache.mu.RUnlock()
	now := time.Now()
	out := make([]*ent.Incident, 0, len(incidentCache.active))
	for _, inc := range incidentCache.active {
		if inc.ExpiresAt.After(now) {
			out = append(out, inc)
		}
	}
	return out
}

// CurrentIncidentScene returns the scene to display and whether one is active.
// The highest-severity incident is shown; the rest are reported as a count.
func CurrentIncidentScene() (render.IncidentScene, bool) {
	active := ActiveIncidents()
	if len(active) == 0 {
		return render.IncidentScene{}, false
	}
	top := active[0]
	return render.IncidentScene{
		Title:    top.Title,
		Message:  top.Message,
		Severity: top.Severity,
		Since:    top.CreatedAt,
		More:     len(active) - 1,
	}, true
}

// ---------------------------------------------------------------------------
// Ingress
// ---------------------------------------------------------------------------

type incidentIngressPayload struct {
	Status      string              `json:"status"`
	Fingerprint string              `json:"fingerprint"`
	Title       string              `json:"title"`
	Message     string              `json:"message"`
	Severity    string              `json:"severity"`
	Source      string              `json:"source"`
	TTLSeconds  int                 `json:"ttl_seconds"`
	Alerts      []alertmanagerAlert `json:"alerts"`
}

type alertmanagerAlert struct {
	Status      string            `json:"status"`
	Fingerprint string            `json:"fingerprint"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	StartsAt    time.Time         `json:"startsAt"`
	EndsAt      time.Time         `json:"endsAt"`
}

type normalizedIncident struct {
	Fingerprint string
	Title       string
	Message     string
	Severity    string
	Source      string
	TTL         time.Duration
	Resolve     bool
}

// normalizeIncidentPayload accepts either the generic shape or an Alertmanager
// v4 notification and returns one entry per incident/alert.
func normalizeIncidentPayload(p incidentIngressPayload, now time.Time) ([]normalizedIncident, error) {
	ttl, err := normalizeIncidentTTL(p.TTLSeconds)
	if err != nil {
		return nil, err
	}

	if len(p.Alerts) > 0 {
		out := make([]normalizedIncident, 0, len(p.Alerts))
		for _, a := range p.Alerts {
			title := pick(a.Labels["alertname"], "Alert")
			message := pick(a.Annotations["summary"], a.Annotations["description"])
			resolve := strings.EqualFold(a.Status, "resolved") ||
				(!a.EndsAt.IsZero() && a.EndsAt.Before(now))
			fp := a.Fingerprint
			if fp == "" {
				fp = fingerprintFor(title, message)
			}
			out = append(out, normalizedIncident{
				Fingerprint: fp,
				Title:       title,
				Message:     message,
				Severity:    normalizeSeverity(a.Labels["severity"]),
				Source:      pick(p.Source, "alertmanager"),
				TTL:         ttl,
				Resolve:     resolve,
			})
		}
		return out, nil
	}

	title := strings.TrimSpace(p.Title)
	message := strings.TrimSpace(p.Message)
	if title == "" && message == "" {
		return nil, fmt.Errorf("incident payload requires title/message or alerts")
	}
	fp := strings.TrimSpace(p.Fingerprint)
	if fp == "" {
		fp = fingerprintFor(title, message)
	}
	return []normalizedIncident{{
		Fingerprint: fp,
		Title:       title,
		Message:     message,
		Severity:    normalizeSeverity(p.Severity),
		Source:      pick(p.Source, "webhook"),
		TTL:         ttl,
		Resolve:     strings.EqualFold(p.Status, "resolved"),
	}}, nil
}

func normalizeIncidentTTL(seconds int) (time.Duration, error) {
	switch {
	case seconds == 0:
		return incidentDefaultTTL, nil
	case time.Duration(seconds)*time.Second < incidentMinTTL:
		return 0, fmt.Errorf("ttl_seconds must be at least %d", int(incidentMinTTL.Seconds()))
	case time.Duration(seconds)*time.Second > incidentMaxTTL:
		return 0, fmt.Errorf("ttl_seconds must be at most %d", int(incidentMaxTTL.Seconds()))
	default:
		return time.Duration(seconds) * time.Second, nil
	}
}

func normalizeSeverity(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "critical":
		return "critical"
	case "info":
		return "info"
	default:
		return "warning"
	}
}

func fingerprintFor(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:16])
}

func pick(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

// incidentEvent is a message lifecycle change produced by applyIncident.
type incidentEvent struct {
	Message Message
	Fired   bool
}

// resolvedIncidentMessage builds the read model for a just-resolved incident.
func resolvedIncidentMessage(inc *ent.Incident, resolvedAt time.Time) Message {
	inc.Active = false
	inc.ResolvedAt = &resolvedAt
	return IncidentToMessage(inc)
}

// applyIncident creates, refreshes, or resolves a single normalized incident.
// It returns the message lifecycle events the caller must emit after
// reloadIncidents so the active-message view is consistent.
func (s *Server) applyIncident(n normalizedIncident) []incidentEvent {
	ctx := s.Ctx
	if n.Resolve {
		existing, err := s.DB.Incident.Query().
			Where(incident.FingerprintEQ(n.Fingerprint), incident.ActiveEQ(true)).
			Only(ctx)
		if err != nil {
			if !ent.IsNotFound(err) {
				slog.Error("incident resolve lookup failed", "source", "incident", "error", err)
			}
			return nil
		}
		now := time.Now()
		if _, err := s.DB.Incident.UpdateOneID(existing.ID).
			SetActive(false).
			SetResolvedAt(now).
			Save(ctx); err != nil {
			slog.Error("incident resolve failed", "source", "incident", "error", err)
			return nil
		}
		return []incidentEvent{{Message: resolvedIncidentMessage(existing, now), Fired: false}}
	}
	existing, err := s.DB.Incident.Query().
		Where(incident.FingerprintEQ(n.Fingerprint)).
		Only(ctx)
	if err != nil {
		if !ent.IsNotFound(err) {
			slog.Error("incident lookup failed", "source", "incident", "error", err)
			return nil
		}
		created, err := s.DB.Incident.Create().
			SetFingerprint(n.Fingerprint).
			SetTitle(n.Title).
			SetMessage(n.Message).
			SetSeverity(n.Severity).
			SetSource(n.Source).
			SetActive(true).
			SetCreatedAt(time.Now()).
			SetExpiresAt(time.Now().Add(n.TTL)).
			Save(ctx)
		if err != nil {
			slog.Error("incident create failed", "source", "incident", "error", err)
			return nil
		}
		return []incidentEvent{{Message: IncidentToMessage(created), Fired: true}}
	}
	updated, err := s.DB.Incident.UpdateOneID(existing.ID).
		SetTitle(n.Title).
		SetMessage(n.Message).
		SetSeverity(n.Severity).
		SetSource(n.Source).
		SetActive(true).
		ClearResolvedAt().
		SetExpiresAt(time.Now().Add(n.TTL)).
		Save(ctx)
	if err != nil {
		slog.Error("incident update failed", "source", "incident", "error", err)
		return nil
	}
	return []incidentEvent{{Message: IncidentToMessage(updated), Fired: true}}
}

// emitIncidentEvents publishes fired/resolved events after the incident cache is
// refreshed so ActiveMessages reflects the new state.
func emitIncidentEvents(events []incidentEvent) {
	for _, ev := range events {
		if ev.Fired {
			emitMessageFired(ev.Message)
		} else {
			emitMessageResolved(ev.Message)
		}
	}
}

// incidentJSON renders an incident for the API.
func incidentJSON(inc *ent.Incident) gin.H {
	return gin.H{
		"id":          inc.ID,
		"fingerprint": inc.Fingerprint,
		"title":       inc.Title,
		"message":     inc.Message,
		"severity":    inc.Severity,
		"source":      inc.Source,
		"created_at":  inc.CreatedAt,
		"expires_at":  inc.ExpiresAt,
	}
}

// ---------------------------------------------------------------------------
// API handlers
// ---------------------------------------------------------------------------

// APIIncidentIngest raises, refreshes, or resolves incidents. Authenticated by
// WebhookAuthMiddleware (X-API-Key / ?token=).
func (s *Server) APIIncidentIngest(c *gin.Context) {
	var p incidentIngressPayload
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(400, gin.H{"error": "invalid JSON body"})
		return
	}
	items, err := normalizeIncidentPayload(p, time.Now())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	var incEvents []incidentEvent
	for _, n := range items {
		incEvents = append(incEvents, s.applyIncident(n)...)
	}
	reloadIncidents(s.DB)
	emitIncidentEvents(incEvents)
	c.JSON(200, gin.H{"ok": true, "active_count": len(ActiveIncidents())})
}

// APIIncidentList lists active incidents (viewer).
func (s *Server) APIIncidentList(c *gin.Context) {
	active := ActiveIncidents()
	out := make([]gin.H, 0, len(active))
	for _, inc := range active {
		out = append(out, incidentJSON(inc))
	}
	c.JSON(200, gin.H{"incidents": out, "count": len(out)})
}

// APIIncidentResolve resolves one incident by id (admin).
func (s *Server) APIIncidentResolve(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid incident id"})
		return
	}
	var resolved *ent.Incident
	if inc, qerr := s.DB.Incident.Query().
		Where(incident.IDEQ(id), incident.ActiveEQ(true)).
		Only(s.Ctx); qerr == nil {
		resolved = inc
	}
	now := time.Now()
	if _, err := s.DB.Incident.Update().
		Where(incident.IDEQ(id), incident.ActiveEQ(true)).
		SetActive(false).
		SetResolvedAt(now).
		Save(s.Ctx); err != nil {
		c.JSON(500, gin.H{"error": "failed to resolve incident"})
		return
	}
	reloadIncidents(s.DB)
	if resolved != nil {
		emitMessageResolved(resolvedIncidentMessage(resolved, now))
	}
	c.JSON(200, gin.H{"ok": true})
}

// ---------------------------------------------------------------------------
// Admin handlers
// ---------------------------------------------------------------------------

// AdminIncidentList renders the incident console.
func (s *Server) AdminIncidentList(c *gin.Context) {
	s.renderPage(c, 200, "incidents.html", gin.H{
		"active":    "incidents",
		"incidents": ActiveIncidents(),
	})
}

// AdminIncidentResolve resolves one incident (form field id).
func (s *Server) AdminIncidentResolve(c *gin.Context) {
	id, err := strconv.Atoi(c.PostForm("id"))
	if err != nil {
		SetFlash(c, "danger", "Invalid incident id")
		c.Redirect(302, "/admin/incidents")
		return
	}
	var resolved *ent.Incident
	if inc, qerr := s.DB.Incident.Query().
		Where(incident.IDEQ(id), incident.ActiveEQ(true)).
		Only(s.Ctx); qerr == nil {
		resolved = inc
	}
	now := time.Now()
	if _, err := s.DB.Incident.Update().
		Where(incident.IDEQ(id), incident.ActiveEQ(true)).
		SetActive(false).
		SetResolvedAt(now).
		Save(s.Ctx); err != nil {
		SetFlash(c, "danger", "Failed to resolve incident")
		c.Redirect(302, "/admin/incidents")
		return
	}
	reloadIncidents(s.DB)
	if resolved != nil {
		emitMessageResolved(resolvedIncidentMessage(resolved, now))
	}
	SetFlash(c, "success", "Incident resolved")
	c.Redirect(302, "/admin/incidents")
}

// AdminIncidentResolveAll resolves every active incident.
func (s *Server) AdminIncidentResolveAll(c *gin.Context) {
	now := time.Now()
	active := ActiveIncidents()
	s.DB.Incident.Update().
		Where(incident.ActiveEQ(true)).
		SetActive(false).
		SetResolvedAt(now).
		SaveX(s.Ctx)
	reloadIncidents(s.DB)
	for _, inc := range active {
		emitMessageResolved(resolvedIncidentMessage(inc, now))
	}
	SetFlash(c, "success", "All incidents resolved")
	c.Redirect(302, "/admin/incidents")
}

// AdminIncidentTest raises a short-lived demo incident.
func (s *Server) AdminIncidentTest(c *gin.Context) {
	now := time.Now()
	created, err := s.DB.Incident.Create().
		SetFingerprint("test:" + now.Format("20060102150405")).
		SetTitle("TEST INCIDENT").
		SetMessage("Raised from the incident console. Resolve it or let it expire.").
		SetSeverity("critical").
		SetSource("admin-test").
		SetActive(true).
		SetCreatedAt(now).
		SetExpiresAt(now.Add(5 * time.Minute)).
		Save(s.Ctx)
	if err != nil {
		SetFlash(c, "danger", "Failed to raise test incident")
		c.Redirect(302, "/admin/incidents")
		return
	}
	reloadIncidents(s.DB)
	emitMessageFired(IncidentToMessage(created))
	SetFlash(c, "success", "Test incident raised")
	c.Redirect(302, "/admin/incidents")
}
