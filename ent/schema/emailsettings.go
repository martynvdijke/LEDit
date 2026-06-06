package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type EmailSettings struct {
	ent.Schema
}

func (EmailSettings) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.String("host"),
		field.Int("port"),
		field.String("username"),
		field.String("password"),
		field.String("from_address"),
		field.Bool("use_tls").Default(true),
	}
}
