package schema

import (
	"regexp"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

var hexColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// Theme is a named render palette. Built-in themes (cyber, f1, untappd) are
// seeded at startup and are immutable; exactly one theme is the global default.
type Theme struct {
	ent.Schema
}

func (Theme) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty().MaxLen(64),
		field.String("bg_color").Default("#282a36").Match(hexColorPattern),
		field.String("accent_color").Default("#50fa7b").Match(hexColorPattern),
		field.String("text_color").Default("#8be9fd").Match(hexColorPattern),
		field.String("title").Default("CUSTOM").MaxLen(64),
		field.Float("font_size").Default(24).Min(8).Max(100),
		field.Bool("built_in").Default(false),
		field.Bool("is_default").Default(false),
		field.String("font_name").Default(""),
	}
}

func (Theme) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("assignments", ThemeAssignment.Type),
	}
}

func (Theme) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("name").Unique(),
	}
}
