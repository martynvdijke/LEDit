package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// Speedtest service-monitor source (Speedtest Tracker).
//
// Credential convention: `token` is the bearer token, sent as
// `Authorization: Bearer <token>`.
type Speedtest struct {
	ent.Schema
}

func (Speedtest) Fields() []ent.Field {
	return []ent.Field{
		field.String("token").Default("").Comment("Speedtest Tracker bearer token"),
		field.String("url").Default("").Comment("Speedtest Tracker base URL"),
	}
}
