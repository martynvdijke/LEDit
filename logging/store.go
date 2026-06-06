package logging

import (
	"context"
	"log"
	"time"

	"ledit/ent"
)

// logEntry represents a queued log entry for database insertion.
type logEntry struct {
	Timestamp time.Time
	Level     string
	Source    string
	Message   string
	Metadata  string
}

// LogStore handles batch insertion of log entries into the database.
type LogStore struct {
	client *ent.Client
	queue  chan logEntry
	done   chan struct{}
}

// NewLogStore creates a LogStore and starts the background batch inserter.
func NewLogStore(client *ent.Client, bufferSize int) *LogStore {
	ls := &LogStore{
		client: client,
		queue:  make(chan logEntry, bufferSize),
		done:   make(chan struct{}),
	}
	go ls.loop()
	return ls
}

// Submit queues a log entry for database insertion.
func (ls *LogStore) Submit(ts time.Time, level, source, message, metadata string) {
	entry := logEntry{
		Timestamp: ts,
		Level:     level,
		Source:    source,
		Message:   message,
		Metadata:  metadata,
	}
	select {
	case ls.queue <- entry:
	default:
		log.Println("log store queue full, dropping entry")
	}
}

// Close stops the background inserter and waits for pending writes.
func (ls *LogStore) Close() {
	close(ls.done)
}

func (ls *LogStore) loop() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var batch []logEntry

	flush := func() {
		if len(batch) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		bulk := make([]*ent.LogEntryCreate, len(batch))
		for i, e := range batch {
			bulk[i] = ls.client.LogEntry.Create().
				SetTimestamp(e.Timestamp).
				SetLevel(e.Level).
				SetSource(e.Source).
				SetMessage(e.Message)
			if e.Metadata != "" {
				bulk[i] = bulk[i].SetMetadata(e.Metadata)
			}
		}
		_, err := ls.client.LogEntry.CreateBulk(bulk...).Save(ctx)
		if err != nil {
			log.Printf("log store flush error: %v", err)
		}
		cancel()
		batch = batch[:0]
	}

	for {
		select {
		case <-ls.done:
			flush()
			return
		case entry := <-ls.queue:
			batch = append(batch, entry)
			if len(batch) >= 50 {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}
