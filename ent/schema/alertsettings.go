package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type AlertSettings struct {
	ent.Schema
}

func (AlertSettings) Fields() []ent.Field {
	return []ent.Field{
		field.Bool("gotify_enabled").Default(false),
		field.String("gotify_url").Default("").Optional(),
		field.String("gotify_token").Default("").Optional(),
		field.Bool("email_enabled").Default(false),
		field.String("recipient_email").Default("").Optional(),
		field.Int("failure_threshold").Default(3),
		field.Int("cooldown_minutes").Default(15),
		field.Int("stale_multiplier").Default(3),
		field.Bool("notify_recovery").Default(true),
	}
}
