package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type OutboundWebhook struct {
	ent.Schema
}

func (OutboundWebhook) Fields() []ent.Field {
	return []ent.Field{
		field.String("url"),
		field.String("secret").Default(""),
		field.Bool("enabled").Default(true),
		field.Time("created_at").Default(time.Now),
	}
}
