package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// Overseerr request-manager source. Also serves Jellyseerr, which implements
// the identical `/api/v1/request/count` + `X-Api-Key` contract.
//
// Credential convention: `token` is the Overseerr/Jellyseerr API key.
type Overseerr struct {
	ent.Schema
}

func (Overseerr) Fields() []ent.Field {
	return []ent.Field{
		field.String("token").Default("").Comment("Overseerr/Jellyseerr API key (X-Api-Key)"),
		field.String("url").Default("").Comment("Overseerr/Jellyseerr base URL"),
	}
}
