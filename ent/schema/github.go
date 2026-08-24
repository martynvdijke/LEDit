package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type GitHub struct {
	ent.Schema
}

func (GitHub) Fields() []ent.Field {
	return []ent.Field{
		field.String("token").Default("").Comment("GitHub repository identifier owner/repo"),
		field.String("url").Default("https://api.github.com/repos/%s").Comment("GitHub API URL with %s for owner/repo"),
	}
}
