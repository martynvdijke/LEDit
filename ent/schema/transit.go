package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type Transit struct {
	ent.Schema
}

func (Transit) Fields() []ent.Field {
	return []ent.Field{
		field.String("token").Default("").Comment("VBB stop ID"),
		field.String("url").Default("https://v6.vbb.transport.rest/stops/%s/departures").Comment("Departures API URL with %s for stop ID"),
	}
}
