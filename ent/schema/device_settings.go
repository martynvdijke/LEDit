package schema

import (
	"errors"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type DeviceSettings struct {
	ent.Schema
}

func (DeviceSettings) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").Default(""),
		field.String("ip").Default(""),
		field.Int("port").Default(6270),
		field.String("username").Default(""),
		field.String("password").Default(""),
		field.Int("width").Default(64),
		field.Int("height").Default(64),
		field.Bool("enabled").Default(true),
		field.String("token").Default(""),
		field.Int("refresh_interval").Default(60),
		field.Time("last_seen_at").Optional().Nillable(),
		field.Int("frames_served").Default(0),
		field.String("content_mode").Default("global").Validate(func(s string) error {
			switch s {
			case "global", "playlist":
				return nil
			default:
				return errors.New("content_mode must be one of global, playlist")
			}
		}),
		field.Int("playlist_id").Optional().Nillable(),
	}
}
