package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

type GeneralSettings struct {
	ent.Schema
}

func (GeneralSettings) Fields() []ent.Field {
	return []ent.Field{
		field.Float("timeout"),
		field.Bool("random"),
		field.Int("width").Default(64),
		field.Int("height").Default(64),
	}
}

func (GeneralSettings) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("sonarr", Sonarr.Type),
		edge.To("radarr", Radarr.Type),
		edge.To("f1", F1.Type),
		edge.To("weather", Weather.Type),
		edge.To("home_assistant", HomeAssistant.Type),
		edge.To("untappd", Untappd.Type),
		edge.To("images", Image.Type),
		edge.To("videos", Video.Type),
	}
}
