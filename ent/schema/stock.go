package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type Stock struct {
	ent.Schema
}

func (Stock) Fields() []ent.Field {
	return []ent.Field{
		field.String("token").Default("AAPL,MSFT,GOOGL").Comment("Stock symbols (comma-separated)"),
		field.String("url").Default(""),
	}
}
