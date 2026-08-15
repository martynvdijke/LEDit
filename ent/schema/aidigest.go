package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// AIDigest holds the definition of an AI-generated news digest. The datasource
// periodically asks the configured LLM to summarize the referenced feeds and
// caches the result for ttl_minutes.
type AIDigest struct {
	ent.Schema
}

func (AIDigest) Fields() []ent.Field {
	return []ent.Field{
		field.String("name"),
		field.String("prompt").Default("").Optional(),
		// sources is a JSON array of feed names (RSS or news) to summarize.
		field.String("sources").Default("[]"),
		field.Int("ttl_minutes").Default(30),
		field.Bool("enabled").Default(true),
	}
}
