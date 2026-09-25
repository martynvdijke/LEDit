package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type Transmission struct {
	ent.Schema
}

func (Transmission) Fields() []ent.Field {
	return []ent.Field{
		field.String("token").Default("").Comment("Transmission RPC password or token"),
		field.String("url").Default("http://transmission:9091/transmission/rpc").Comment("Transmission RPC URL"),
	}
}
