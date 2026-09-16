package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// DeliveryLog records every outbound delivery attempt (MQTT publish, webhook
// POST) and device acknowledgement. Bounded by a row cap and TTL, pruned on
// insert by DeliveryLogWriter; history survives restarts unlike the legacy
// in-memory webhook ring.
type DeliveryLog struct {
	ent.Schema
}

func (DeliveryLog) Fields() []ent.Field {
	return []ent.Field{
		field.String("message_id").Default(""),
		field.String("kind").Default(""),
		field.Enum("surface").Values("ws", "trmnl", "mqtt", "webhook", "inbound"),
		field.String("target").Default(""),
		field.Enum("status").Values("delivered", "failed", "acked"),
		field.Time("attempted_at").Default(time.Now),
		field.String("error").Default(""),
	}
}
