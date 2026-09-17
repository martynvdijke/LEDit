package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ThemeAssignment is a per-datasource theme override, keyed by the same
// "<type>:<id>" identity the source index uses (e.g. "weather:3", "matrix:1").
type ThemeAssignment struct {
	ent.Schema
}

func (ThemeAssignment) Fields() []ent.Field {
	return []ent.Field{
		field.String("target_type").NotEmpty(),
		field.Int("target_id"),
	}
}

func (ThemeAssignment) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("theme", Theme.Type).Ref("assignments").Unique().Required(),
	}
}

func (ThemeAssignment) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("target_type", "target_id").Unique(),
	}
}
