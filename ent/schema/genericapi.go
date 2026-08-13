package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// GenericAPI is a pluggable datasource for any public JSON API. Config holds
// JSON with an optional title, extra headers and row mappings ({label, path})
// extracted from the response via dot-paths.
type GenericAPI struct {
	ent.Schema
}

func (GenericAPI) Fields() []ent.Field {
	return []ent.Field{
		field.String("token").Default(""),
		field.String("url"),
		field.Text("config").Default("{}").Comment("JSON config: title, headers, rows"),
	}
}
