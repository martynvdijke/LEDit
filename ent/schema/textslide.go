package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type TextSlide struct {
	ent.Schema
}

func (TextSlide) Fields() []ent.Field {
	return []ent.Field{
		field.String("content"),
		field.String("color").Default("#FFFFFF"),
		field.String("bg_color").Default("#000000"),
		field.Int("font_size").Default(32),
	}
}
