package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type Parcel struct {
	ent.Schema
}

func (Parcel) Fields() []ent.Field {
	return []ent.Field{
		field.String("token").Default("").Comment("17Track/AfterShip API key"),
		field.String("url").Default("").Comment("Parcel tracking API URL"),
	}
}
