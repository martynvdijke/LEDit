package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// FirmwareRelease is an immutable, versioned firmware artifact. Correcting a
// build means registering a new version, never editing an existing row.
type FirmwareRelease struct {
	ent.Schema
}

func (FirmwareRelease) Fields() []ent.Field {
	return []ent.Field{
		field.String("version"),
		field.String("channel").Default("stable"),
		field.String("sha256"),
		field.Int("size_bytes").Default(0),
		field.String("artifact_path"),
		field.String("notes").Default(""),
		field.Bool("mandatory").Default(false),
		field.String("min_version").Default(""),
		field.Bool("enabled").Default(true),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (FirmwareRelease) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("channel", "version").Unique(),
	}
}
