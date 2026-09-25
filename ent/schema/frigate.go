package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type Frigate struct {
	ent.Schema
}

func (Frigate) Fields() []ent.Field {
	return []ent.Field{
		field.String("token").Default("").Comment("Frigate API token"),
		field.String("url").Default("http://frigate:5000/api/stats").Comment("Frigate stats API URL"),
	}
}
