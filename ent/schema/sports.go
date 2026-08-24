package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type Sports struct {
	ent.Schema
}

func (Sports) Fields() []ent.Field {
	return []ent.Field{
		field.String("token").Default("").Comment("ESPN league slug"),
		field.String("url").Default("https://site.api.espn.com/apis/site/v2/sports/%s/scoreboard").Comment("ESPN scoreboard API URL with %s for league slug"),
	}
}
