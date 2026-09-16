package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// GuestPhoto is a photo uploaded through the photo-scoped guest frame page and
// held for admin moderation. path points at the stored file under
// web/media/guest_uploads. guest_token_id records the uploading guest token (0
// when unavailable). status is pending/approved/rejected; bytes is the stored
// size; expires_at bounds retention so an upload flood cannot fill the disk.
//
// Migration: ent auto-creates/updates the table on startup via
// client.Schema.Create. Reversible — removing this schema drops the table.
type GuestPhoto struct {
	ent.Schema
}

func (GuestPhoto) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.String("path").Default(""),
		field.Int("guest_token_id").Default(0),
		field.Enum("status").Values("pending", "approved", "rejected").Default("pending"),
		field.Int("bytes").Default(0),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
		field.Time("expires_at").Optional().Nillable(),
	}
}

func (GuestPhoto) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status"),
		index.Fields("expires_at"),
	}
}
