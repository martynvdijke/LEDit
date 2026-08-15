package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type Countdown struct {
	ent.Schema
}

func (Countdown) Fields() []ent.Field {
	return []ent.Field{
		field.String("name"),
		field.Time("target_time"),
		field.String("label").Default("").Optional(),
		field.Bool("enabled").Default(true),
	}
}
