package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type Sonarr struct {
	ent.Schema
}

func (Sonarr) Fields() []ent.Field {
	return []ent.Field{
		field.String("token").Default(""),
		field.String("url").Default(""),
	}
}
