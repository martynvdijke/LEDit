package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type DatasourcePlugin struct {
	ent.Schema
}

func (DatasourcePlugin) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").Unique(),
		field.Enum("kind").Values("exec", "http"),
		field.String("target"),
		field.Bool("enabled").Default(false),
		field.Int("timeout_ms").Default(3000).Min(100).Max(60000),
		field.Time("created_at").Default(time.Now).Immutable(),
	}
}
