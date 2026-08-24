package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type Uptime struct {
	ent.Schema
}

func (Uptime) Fields() []ent.Field {
	return []ent.Field{
		field.String("url").Comment("Base URL for probes (unused, kept for consistency)"),
		field.Text("config").Default("[]").Comment("JSON array of probe targets"),
	}
}
