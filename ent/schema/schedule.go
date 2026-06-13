package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type Schedule struct {
	ent.Schema
}

func (Schedule) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").Default(""),
		field.String("time_range").Default("").Comment("Time range like 08:00-12:00"),
		field.Bool("enabled").Default(true),
	}
}
