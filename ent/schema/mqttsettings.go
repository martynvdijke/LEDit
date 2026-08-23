package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type MQTTSettings struct {
	ent.Schema
}

func (MQTTSettings) Fields() []ent.Field {
	return []ent.Field{
		field.Bool("enabled").Default(false),
		field.String("broker").Default(""),
		field.String("username").Default(""),
		field.String("password").Default(""),
		field.String("control_topic").Default("ledit/control"),
		field.String("display_topic").Default("ledit/display"),
	}
}
