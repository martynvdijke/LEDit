package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type Untappd struct {
	ent.Schema
}

func (Untappd) Fields() []ent.Field {
	return []ent.Field{
		field.String("token").Default(""),
		field.String("url").Default(""),
	}
}
