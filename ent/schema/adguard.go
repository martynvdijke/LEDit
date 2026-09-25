package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type AdGuard struct {
	ent.Schema
}

func (AdGuard) Fields() []ent.Field {
	return []ent.Field{
		field.String("token").Default("").Comment("AdGuard Home API token or basic auth"),
		field.String("url").Default("http://adguard:3000/control/stats").Comment("AdGuard Home stats API URL"),
	}
}
