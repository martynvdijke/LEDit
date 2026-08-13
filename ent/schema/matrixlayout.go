package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// MatrixLayout holds a dashboard grid definition. Bindings is a JSON array of
// {row, col, source_type, source_id} entries that map cells to datasources.
type MatrixLayout struct {
	ent.Schema
}

func (MatrixLayout) Fields() []ent.Field {
	return []ent.Field{
		field.String("name"),
		field.Int("rows").Default(2),
		field.Int("cols").Default(2),
		field.Int("gap").Default(2),
		field.String("background").Default("#282a36"),
		field.Bool("enabled").Default(true),
		field.Text("bindings").Default("[]").Comment("JSON array of {row, col, source_type, source_id}"),
	}
}
