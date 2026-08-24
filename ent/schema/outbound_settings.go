package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type OutboundSettings struct {
	ent.Schema
}

func (OutboundSettings) Fields() []ent.Field {
	return []ent.Field{
		field.Bool("mqtt_publish_enabled").Default(false),
		field.Bool("metrics_enabled").Default(false),
		field.Bool("webhooks_enabled").Default(false),
		field.Bool("ha_discovery_enabled").Default(false),
	}
}
