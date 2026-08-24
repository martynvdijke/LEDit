package schema

import (
	"errors"
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type DeviceGroup struct {
	ent.Schema
}

func (DeviceGroup) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty().Validate(func(s string) error {
			if len(s) < 1 || len(s) > 64 {
				return errors.New("name must be 1-64 chars")
			}
			return nil
		}),
		field.String("description").Default("").MaxLen(256),
		field.Time("created_at").Default(time.Now),
		field.String("content_mode").Default("global").Validate(func(s string) error {
			switch s {
			case "global", "playlist", "scheduled":
				return nil
			default:
				return errors.New("content_mode must be one of global, playlist, scheduled")
			}
		}),
		field.Int("playlist_id").Optional().Nillable(),
		field.Text("scheduled_playlist_ids").Default("[]"),
		field.Int("fallback_playlist_id").Optional().Nillable(),
	}
}

func (DeviceGroup) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("devices", DeviceSettings.Type),
	}
}

func (DeviceGroup) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("name").Unique(),
	}
}
