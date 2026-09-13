package handlers

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"ledit/ent"
	"ledit/ent/deliverylog"
	"ledit/ent/predicate"
)

// DeliveryLogEntry is one outbound delivery attempt or acknowledgement.
type DeliveryLogEntry struct {
	MessageID   string
	Kind        string
	Surface     string
	Target      string
	Status      string
	AttemptedAt time.Time
	Error       string
}

// DeliveryLogWriter appends to the bounded delivery_log table and prunes on
// insert. maxRows/ttl are fields (defaults 1000 / 7d) so tests need not write
// a thousand rows.
type DeliveryLogWriter struct {
	db      func() *ent.Client
	maxRows int
	ttl     time.Duration
}

func NewDeliveryLogWriter(db func() *ent.Client) *DeliveryLogWriter {
	return &DeliveryLogWriter{db: db, maxRows: 1000, ttl: 7 * 24 * time.Hour}
}

// GlobalDeliveryLog is the process-wide writer, wired in InitOutbound.
var GlobalDeliveryLog *DeliveryLogWriter

func recordDelivery(e DeliveryLogEntry) {
	if GlobalDeliveryLog != nil {
		GlobalDeliveryLog.Record(e)
	}
}

// Record inserts one row then prunes beyond-cap / beyond-TTL rows. Any error
// (including "database is locked") is logged at debug and the entry dropped;
// delivery must never block on the log.
func (w *DeliveryLogWriter) Record(e DeliveryLogEntry) {
	if w == nil || w.db == nil {
		return
	}
	client := w.db()
	if client == nil {
		return
	}
	if e.AttemptedAt.IsZero() {
		e.AttemptedAt = time.Now()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := client.DeliveryLog.Create().
		SetMessageID(e.MessageID).
		SetKind(e.Kind).
		SetSurface(deliverylog.Surface(e.Surface)).
		SetTarget(e.Target).
		SetStatus(deliverylog.Status(e.Status)).
		SetAttemptedAt(e.AttemptedAt).
		SetError(e.Error).
		Save(ctx)
	if err != nil {
		slog.Debug("delivery log entry dropped", "surface", e.Surface, "error", err)
		return
	}
	w.prune(ctx, client)
}

// prune deletes rows older than the TTL plus any beyond maxRows newest.
func (w *DeliveryLogWriter) prune(ctx context.Context, client *ent.Client) {
	cutoff := time.Now().Add(-w.ttl)
	ids, err := client.DeliveryLog.Query().
		Order(ent.Desc(deliverylog.FieldAttemptedAt), ent.Desc(deliverylog.FieldID)).
		Limit(w.maxRows).
		IDs(ctx)
	if err != nil {
		return
	}
	preds := []predicate.DeliveryLog{deliverylog.AttemptedAtLT(cutoff)}
	if len(ids) > 0 {
		preds = append(preds, deliverylog.IDNotIn(ids...))
	}
	if _, err := client.DeliveryLog.Delete().Where(deliverylog.Or(preds...)).Exec(ctx); err != nil {
		slog.Debug("delivery log prune failed", "error", err)
	}
}

// APIDeliveryLog returns recent delivery rows plus per-device ack state.
func (s *Server) APIDeliveryLog(c *gin.Context) {
	limit := 50
	if raw := c.Query("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := s.DB.DeliveryLog.Query().
		Order(ent.Desc(deliverylog.FieldAttemptedAt), ent.Desc(deliverylog.FieldID)).
		Limit(limit).
		All(c.Request.Context())
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to load delivery log"})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"deliveries": rows, "device_acks": DeviceAcks()})
}

// AdminDeliveryLog renders the delivery/ack console.
func (s *Server) AdminDeliveryLog(c *gin.Context) {
	rows, _ := s.DB.DeliveryLog.Query().
		Order(ent.Desc(deliverylog.FieldAttemptedAt), ent.Desc(deliverylog.FieldID)).
		Limit(100).
		All(s.Ctx)
	s.renderPage(c, 200, "delivery.html", gin.H{
		"deliveries": rows,
		"acks":       DeviceAcks(),
	})
}
