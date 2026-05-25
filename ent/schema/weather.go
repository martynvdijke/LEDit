package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type Weather struct {
	ent.Schema
}

func (Weather) Fields() []ent.Field {
	return []ent.Field{
		field.String("token").Default(""),
		field.String("url").Default(""),
	}
}
