package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// DisplayRule is an event-driven rule that pins a datasource when its
// condition is satisfied.
type DisplayRule struct {
	ent.Schema
}

func (DisplayRule) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").Default(""),
		field.Bool("enabled").Default(true),
		field.String("source_type").Default(""),
		field.Int("source_id").Default(0),
		field.Text("condition").Default("{}"),
		field.Int("check_interval_seconds").Default(30).Min(5),
		field.Int("cooldown_seconds").Default(0).Min(0),
	}
}
