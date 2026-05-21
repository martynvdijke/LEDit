package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type HomeAssistant struct {
	ent.Schema
}

func (HomeAssistant) Fields() []ent.Field {
	return []ent.Field{
		field.String("token").Default(""),
		field.String("url").Default(""),
	}
}
