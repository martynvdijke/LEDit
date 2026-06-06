package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type UmamiSettings struct {
	ent.Schema
}

func (UmamiSettings) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.String("endpoint"),
		field.String("website_id"),
		field.Bool("enable").Default(false),
	}
}
