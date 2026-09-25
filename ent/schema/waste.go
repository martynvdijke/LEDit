package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type Waste struct {
	ent.Schema
}

func (Waste) Fields() []ent.Field {
	return []ent.Field{
		field.String("token").Default("").Comment("unused"),
		field.String("url").Default("").Comment("ICS URL for waste/bin collection"),
	}
}
