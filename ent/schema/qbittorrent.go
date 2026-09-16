package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// Qbittorrent download-client source.
//
// Credential convention: `token` holds `username:password` (split on the first
// colon) because qBittorrent WebUI only supports cookie-session login.
type Qbittorrent struct {
	ent.Schema
}

func (Qbittorrent) Fields() []ent.Field {
	return []ent.Field{
		field.String("token").Default("").Comment("username:password for /api/v2/auth/login"),
		field.String("url").Default("").Comment("qBittorrent WebUI base URL"),
	}
}
