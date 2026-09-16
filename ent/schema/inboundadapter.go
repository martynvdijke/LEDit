package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// InboundAdapter holds per-kind configuration for an inbound messaging bridge
// (discord, slack, matrix, ntfy, gotify, pushover). Adapters are inert until
// enabled and configured: secret is the shared credential (bot/access token or
// signing/webhook secret, depending on kind), allowlist is a JSON array of
// channel/room/guild IDs (a non-empty list is required for adapters that gate
// senders), and config is a JSON object of kind-specific options (homeserver
// URL, since cursor, etc). Secrets are never returned to the admin API.
//
// Migration: ent auto-creates/updates the table on startup via
// client.Schema.Create. Reversible — removing this schema drops the table.
type InboundAdapter struct {
	ent.Schema
}

func (InboundAdapter) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.String("kind").Unique(),
		field.Bool("enabled").Default(false),
		field.String("secret").Default(""),
		field.Text("allowlist").Default("[]"),
		field.Text("config").Default("{}"),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
