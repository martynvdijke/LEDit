package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type LogEntry struct {
	ent.Schema
}

func (LogEntry) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.Time("timestamp"),
		field.String("level"),
		field.String("source"),
		field.String("message"),
		field.String("metadata").Optional(),
	}
}

func (LogEntry) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("level", "timestamp"),
	}
}
