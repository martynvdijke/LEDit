package schema

import (
	"errors"
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// Incident is a raised monitoring alert that takes over the display until it is
// resolved or expires. Fingerprint is the dedupe key assigned by the source
// (or derived from the title/message for generic payloads).
type Incident struct {
	ent.Schema
}

func (Incident) Fields() []ent.Field {
	return []ent.Field{
		field.String("fingerprint").Unique(),
		field.String("title").Default(""),
		field.String("message").Default(""),
		field.String("severity").Default("warning").Validate(func(s string) error {
			switch s {
			case "critical", "warning", "info":
				return nil
			default:
				return errors.New("severity must be one of critical, warning, info")
			}
		}),
		field.String("source").Default(""),
		field.Bool("active").Default(true),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
		field.Time("resolved_at").Optional().Nillable(),
		field.Time("expires_at"),
	}
}
