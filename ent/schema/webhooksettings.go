package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type WebhookSettings struct {
	ent.Schema
}

func (WebhookSettings) Fields() []ent.Field {
	return []ent.Field{
		field.String("api_key").Default(""),
		field.Int("default_ttl").Default(30).Min(1).Max(3600),
	}
}
