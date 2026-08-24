package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type TimelapseFrame struct {
	ent.Schema
}

func (TimelapseFrame) Fields() []ent.Field {
	return []ent.Field{
		field.Int("device_id"),
		field.Time("captured_at").Default(time.Now),
		field.String("source_type"),
		field.Int("source_id"),
		field.String("source_label"),
		field.String("file_path"),
		field.Int("width"),
		field.Int("height"),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (TimelapseFrame) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("device_id", "captured_at"),
	}
}
