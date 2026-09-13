package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// Scene is an ambient takeover: compound sensor triggers resolved by the event
// evaluator, plus a bundled set of actions (pin source/playlist, brightness,
// overlay) applied atomically while the triggers hold or until TTLSeconds.
type Scene struct {
	ent.Schema
}

func (Scene) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty().Unique(),
		field.Bool("enabled").Default(true),
		// JSON: [{op:"all-of"|"any-of", conditions:[{entity_id,operator,value}]}]
		field.Text("triggers").Default("[]"),
		// JSON: {source_type,source_id,playlist_id,brightness_level,overlay_text}
		field.Text("actions").Default("{}"),
		field.Int("priority").Default(0),
		field.Int("ttl_seconds").Optional().Nillable().Min(0).Max(86400),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Scene) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("general_settings", GeneralSettings.Type).Ref("scenes").Unique(),
	}
}
