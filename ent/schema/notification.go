package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type Notification struct {
	ent.Schema
}

func (Notification) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.String("title").Default(""),
		field.String("message").Default(""),
		field.Time("created_at"),
	}
}
