package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type Jellyfin struct {
	ent.Schema
}

func (Jellyfin) Fields() []ent.Field {
	return []ent.Field{
		field.String("token").Default("").Comment("Jellyfin API token (X-Emby-Token)"),
		field.String("url").Comment("Jellyfin server base URL"),
	}
}
