package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type Crypto struct {
	ent.Schema
}

func (Crypto) Fields() []ent.Field {
	return []ent.Field{
		field.String("token").Default("bitcoin, ethereum").Comment("CoinGecko coin IDs (comma-separated)"),
		field.String("url").Default(""),
	}
}
