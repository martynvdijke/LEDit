package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// UptimeKuma service-monitor source.
//
// Credential convention: `token` is the public status-page slug (no
// credentials are used; the status page must be published). Only the public
// `/api/status-page/*` endpoints are read.
type UptimeKuma struct {
	ent.Schema
}

func (UptimeKuma) Fields() []ent.Field {
	return []ent.Field{
		field.String("token").Default("").Comment("public status-page slug"),
		field.String("url").Default("").Comment("Uptime Kuma base URL"),
	}
}
