package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type AdminSettings struct {
	ent.Schema
}

func (AdminSettings) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.String("username").Default("admin"),
		field.String("password_hash").Default(""),
	}
}
