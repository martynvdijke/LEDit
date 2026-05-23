package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type RssFeed struct {
	ent.Schema
}

func (RssFeed) Fields() []ent.Field {
	return []ent.Field{
		field.String("url"),
		field.String("name").Default(""),
	}
}
