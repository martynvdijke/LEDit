package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type PiHole struct {
	ent.Schema
}

func (PiHole) Fields() []ent.Field {
	return []ent.Field{
		field.String("token").Default("").Comment("Pi-hole API token"),
		field.String("url").Default("http://pi.hole/admin/api.php?summary").Comment("Pi-hole summary API URL"),
	}
}
