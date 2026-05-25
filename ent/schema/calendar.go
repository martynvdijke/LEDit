package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type Calendar struct {
	ent.Schema
}

func (Calendar) Fields() []ent.Field {
	return []ent.Field{
		field.String("url"),
		field.String("name").Default(""),
	}
}
