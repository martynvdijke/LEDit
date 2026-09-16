package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// Immich photo-frame source.
//
// Credential convention: `token` is the Immich API key, sent as the
// `x-api-key` header. `url` is the Immich base URL. `config` is a JSON object
// (`{"mode":"memories|random|album","album":"<id>","interval_seconds":300,
// "slideshow":true}`) rendered by the `has_config` admin form textarea.
type Immich struct {
	ent.Schema
}

func (Immich) Fields() []ent.Field {
	return []ent.Field{
		field.String("token").Default("").Comment("Immich API key (x-api-key header)"),
		field.String("url").Default("").Comment("Immich base URL"),
		field.Text("config").Default("{}").Comment("JSON config: mode/album/interval_seconds/slideshow"),
	}
}
