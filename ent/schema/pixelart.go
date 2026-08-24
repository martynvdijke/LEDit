package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// PixelArt is a user-drawn pixel artwork datasource. Frames holds a JSON
// document (palette + frames), bindings a JSON document of API-driven rules
// (color slots, frame selection, text overlays). api_url/api_token configure
// the optional JSON endpoint that feeds those bindings.
type PixelArt struct {
	ent.Schema
}

func (PixelArt) Fields() []ent.Field {
	return []ent.Field{
		field.String("name"),
		field.Int("grid_width").Default(32),
		field.Int("grid_height").Default(32),
		field.Text("frames").Default("{}").Comment("JSON: palette + frames"),
		field.Text("bindings").Default("{}").Comment("JSON: colorSlots, frameRules, overlays"),
		field.String("api_url").Default(""),
		field.String("api_token").Default(""),
		field.Bool("enabled").Default(true),
		field.Bool("is_draft").Default(false),
	}
}
