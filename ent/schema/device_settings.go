package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type DeviceSettings struct {
	ent.Schema
}

func (DeviceSettings) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").Default(""),
		field.String("ip").Default(""),
		field.Int("port").Default(6270),
		field.String("username").Default(""),
		field.String("password").Default(""),
		field.Int("width").Default(64),
		field.Int("height").Default(64),
		field.Bool("enabled").Default(true),
	}
}
