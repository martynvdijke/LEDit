package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ApiToken holds a hashed bearer token used to authorize API mutations.
//
// The raw secret is shown to the owner exactly once at creation and is never
// stored; only its SHA-256 hash is persisted (token_hash). Tokens are
// revocable (revoked_at) and may expire (expires_at). token_prefix stores the
// first characters of the secret so owners can identify tokens in listings
// without exposing the full secret.
//
// Migration: ent auto-creates/updates the table on startup via
// client.Schema.Create. The change is reversible — removing this schema and
// regenerating drops the apitokens table (no data is referenced elsewhere).
type ApiToken struct {
	ent.Schema
}

func (ApiToken) Fields() []ent.Field {
	return []ent.Field{
		field.Int("id"),
		field.String("name").Default(""),
		field.String("token_hash").Unique(),
		field.String("token_prefix").Default(""),
		field.Int("owner_id").Default(0),
		field.Time("created_at"),
		field.Time("expires_at").Optional().Nillable(),
		field.Time("revoked_at").Optional().Nillable(),
		field.Time("last_used_at").Optional().Nillable(),
		field.Enum("role").Values("admin", "viewer").Default("admin"),
	}
}

func (ApiToken) Indexes() []ent.Index {
	return []ent.Index{
		// Lookups by owner (token lifecycle listing) and by hash (bearer auth).
		index.Fields("owner_id"),
	}
}
