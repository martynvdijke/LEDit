package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type F1 struct {
	ent.Schema
}

func (F1) Fields() []ent.Field {
	return []ent.Field{
		field.String("token").Default(""),
		field.String("url").Default(""),
	}
}
