package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// Playlist is an ordered list of datasource items that can be assigned to a
// device. Items is a JSON array with shape
// [{"source_type":"weather","source_id":3},{"source_type":"builtin","source_id":"analog-clock"}].
type Playlist struct {
	ent.Schema
}

func (Playlist) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").Default(""),
		field.Bool("enabled").Default(true),
		field.Text("items").Default("[]").Comment(`JSON: [{"source_type":"weather","source_id":3},{"source_type":"builtin","source_id":"analog-clock"}]`),
	}
}
