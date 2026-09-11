package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type Countdown struct {
	ent.Schema
}

func (Countdown) Fields() []ent.Field {
	return []ent.Field{
		field.String("name"),
		field.Time("target_time"),
		field.String("label").Default("").MaxLen(64).Optional(),
		field.Bool("enabled").Default(true),
		field.Enum("granularity").Values("seconds", "minutes", "hours", "days").Default("seconds"),
		field.Enum("direction").Values("down", "up").Default("down"),
		field.Enum("completion").NamedValues(
			"Now", "now",
			"MessageText", "message",
			"Hide", "hide",
		).Default("now"),
		field.String("completion_message").Default("").MaxLen(32).Optional(),
		field.String("timezone").Default("").Optional(),
	}
}
