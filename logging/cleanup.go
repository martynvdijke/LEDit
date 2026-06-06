package logging

import (
	"context"
	"log"
	"time"

	"ledit/ent"
	"ledit/ent/logentry"
)

// LogCleanup runs periodic cleanup of old log entries based on retention days.
type LogCleanup struct {
	client   *ent.Client
	interval time.Duration
	done     chan struct{}
}

// NewLogCleanup starts a background goroutine that deletes expired log entries.
func NewLogCleanup(client *ent.Client, interval time.Duration) *LogCleanup {
	lc := &LogCleanup{
		client:   client,
		interval: interval,
		done:     make(chan struct{}),
	}
	go lc.loop()
	return lc
}

// Stop terminates the cleanup goroutine.
func (lc *LogCleanup) Stop() {
	close(lc.done)
}

func (lc *LogCleanup) loop() {
	ticker := time.NewTicker(lc.interval)
	defer ticker.Stop()

	lc.cleanup()

	for {
		select {
		case <-lc.done:
			return
		case <-ticker.C:
			lc.cleanup()
		}
	}
}

func (lc *LogCleanup) cleanup() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	retentionDays := 7
	settings, err := lc.client.LogSettings.Query().Only(ctx)
	if err == nil && settings != nil {
		retentionDays = settings.RetentionDays
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	deleted, err := lc.client.LogEntry.Delete().
		Where(logentry.TimestampLT(cutoff)).
		Exec(ctx)
	if err != nil {
		log.Printf("log cleanup error: %v", err)
		return
	}
	if deleted > 0 {
		log.Printf("log cleanup: deleted %d entries older than %d days", deleted, retentionDays)
	}
}
