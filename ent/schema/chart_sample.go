package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type ChartSample struct {
	ent.Schema
}

func (ChartSample) Fields() []ent.Field {
	return []ent.Field{
		field.String("source_type"),
		field.String("source_id"),
		field.Time("sampled_at").Default(time.Now),
		field.Float("value"),
		field.Float("open").Optional().Nillable(),
		field.Float("high").Optional().Nillable(),
		field.Float("low").Optional().Nillable(),
		field.Float("close").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}

func (ChartSample) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("source_type", "source_id", "sampled_at"),
	}
}
