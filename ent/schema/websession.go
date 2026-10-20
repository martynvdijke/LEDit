package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// WebSession persists an authenticated browser session so logins survive a
// process restart. Only the SHA-256 hash of the random cookie token is stored;
// the raw token lives in the HttpOnly cookie. Rows are deleted on logout,
// password reset, or user deletion and are ignored once expiry has passed.
type WebSession struct {
	ent.Schema
}

func (WebSession) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.String("token_hash").Unique(),
		field.Int("user_id"),
		field.String("username").Default(""),
		field.String("role").Default(""),
		field.Time("expiry"),
		field.Time("created_at").Default(time.Now),
	}
}

func (WebSession) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id"),
		index.Fields("expiry"),
	}
}
