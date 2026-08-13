package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// NewsFeed aggregates one or more RSS/Atom feeds (comma-separated URLs) into
// a single scrolling headline source.
type NewsFeed struct {
	ent.Schema
}

func (NewsFeed) Fields() []ent.Field {
	return []ent.Field{
		field.String("url"),
		field.String("name").Default(""),
	}
}
