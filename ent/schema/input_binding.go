package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// InputBinding maps a physical input event (button, encoder, tag, presence,
// lux) to an existing action. A nil device_id scopes the binding to any
// device; resolution orders by order, then device-scoped before global, then id.
type InputBinding struct {
	ent.Schema
}

func (InputBinding) Fields() []ent.Field {
	return []ent.Field{
		field.Int("device_id").Optional().Nillable(),
		field.String("source").Default(""),
		field.String("event").Default(""),
		field.Text("match").Default(""),
		field.Text("action").Default("{}"),
		field.Bool("enabled").Default(true),
		field.Int("order").Default(0),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
