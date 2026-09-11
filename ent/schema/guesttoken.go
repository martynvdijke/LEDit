package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// GuestToken holds a hashed, scoped, revocable credential for the guest remote.
//
// It is deliberately separate from ApiToken: a guest token carries no admin or
// viewer role and can never satisfy an authenticated route. The raw secret is
// shown to the admin exactly once at creation and never stored; only its
// SHA-256 hash is persisted (token_hash). Scopes is a non-empty subset of
// pause/next/message. Tokens may expire (expires_at) and be revoked
// (revoked_at). token_prefix stores the first characters of the secret so
// admins can identify tokens in listings without exposing the full secret.
//
// Migration: ent auto-creates/updates the table on startup via
// client.Schema.Create. The change is reversible — removing this schema and
// regenerating drops the guesttokens table (no data is referenced elsewhere).
type GuestToken struct {
	ent.Schema
}

func (GuestToken) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.String("label").MaxLen(64).Default(""),
		field.String("token_hash").Unique(),
		field.String("token_prefix").Default(""),
		field.Strings("scopes").Default([]string{"pause", "next", "message"}),
		field.Time("created_at").Default(time.Now),
		field.Time("expires_at").Optional().Nillable(),
		field.Time("revoked_at").Optional().Nillable(),
		field.Time("last_used_at").Optional().Nillable(),
	}
}

func (GuestToken) Indexes() []ent.Index {
	return []ent.Index{
		// token_hash is unique and therefore already indexed for auth lookup.
		index.Fields("revoked_at"),
	}
}
