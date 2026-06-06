package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type AISettings struct {
	ent.Schema
}

func (AISettings) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.String("provider"),
		field.String("api_key"),
		field.String("model"),
		field.String("endpoint").Optional(),
	}
}
