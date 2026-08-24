package schema

import (
	"regexp"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type User struct {
	ent.Schema
}

func (User) Fields() []ent.Field {
	return []ent.Field{
		field.String("username").
			MinLen(3).
			MaxLen(64).
			Match(regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)),
		field.String("password_hash"),
		field.Enum("role").Values("admin", "viewer").Default("viewer"),
		field.Time("created_at"),
		field.Time("last_login_at").Optional().Nillable(),
	}
}

func (User) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("username").Unique(),
	}
}
