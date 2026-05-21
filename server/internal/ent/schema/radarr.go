package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type Radarr struct {
	ent.Schema
}

func (Radarr) Fields() []ent.Field {
	return []ent.Field{
		field.String("token").Default(""),
		field.String("url").Default(""),
	}
}
