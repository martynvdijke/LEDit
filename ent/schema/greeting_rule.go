package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// GreetingRule stores a presence-driven greeting.
type GreetingRule struct {
	ent.Schema
}

func (GreetingRule) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty(),
		field.String("entity_path").NotEmpty(),
		field.String("match_value").Default("home"),
		field.String("match_operator").Default("eq"),
		field.String("message_template").NotEmpty(),
		field.Int("ttl_seconds").Default(30).Min(5).Max(300),
		field.Int("cooldown_minutes").Default(30).Min(1).Max(1440),
		field.String("quiet_hours_start").Optional().Nillable(),
		field.String("quiet_hours_end").Optional().Nillable(),
		field.Bool("enabled").Default(true),
		field.Time("last_triggered_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
