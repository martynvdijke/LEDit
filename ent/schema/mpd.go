package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type MPD struct {
	ent.Schema
}

func (MPD) Fields() []ent.Field {
	return []ent.Field{
		field.String("host").Default(""),
		field.Int("port").Default(6600).Min(1).Max(65535),
		field.String("password").Default(""),
		field.Bool("enabled").Default(false),
	}
}
