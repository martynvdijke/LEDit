package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type Image struct {
	ent.Schema
}

func (Image) Fields() []ent.Field {
	return []ent.Field{
		field.String("path").Comment("Path to the image file on disk"),
	}
}
