package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type LogSettings struct {
	ent.Schema
}

func (LogSettings) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.String("verbosity").Default("warn"),
		field.Int("retention_days").Default(7),
		field.String("otel_endpoint").Optional(),
		field.String("otel_protocol").Optional(),
		field.Bool("otel_enabled").Default(false),
	}
}
