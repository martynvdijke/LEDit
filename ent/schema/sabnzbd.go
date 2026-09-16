package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// Sabnzbd download-client source.
//
// Credential convention: `token` is the SABnzbd API key, appended as the
// `apikey` query parameter (SABnzbd ignores the X-API-Key header).
type Sabnzbd struct {
	ent.Schema
}

func (Sabnzbd) Fields() []ent.Field {
	return []ent.Field{
		field.String("token").Default("").Comment("SABnzbd API key (apikey query parameter)"),
		field.String("url").Default("").Comment("SABnzbd base URL"),
	}
}
