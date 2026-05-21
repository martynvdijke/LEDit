package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type Video struct {
	ent.Schema
}

func (Video) Fields() []ent.Field {
	return []ent.Field{
		field.String("path").Comment("Path to the video file on disk"),
	}
}
