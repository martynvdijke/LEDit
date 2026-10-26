package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// EffectPreset is a named, reusable instance of a procedural effect from the
// effects registry. Params holds the versioned behaviour document (speed,
// intensity, density, direction, seed) and never resolved theme colors: the
// palette is mapped in at render time from the effective theme.
type EffectPreset struct {
	ent.Schema
}

func (EffectPreset) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty().MaxLen(64).Unique(),
		field.String("effect"),
		field.Text("params").Default("{}").Comment("JSON: versioned effect parameters"),
		field.Bool("built_in").Default(false),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
