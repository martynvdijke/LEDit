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
		field.Bool("brightness_enabled").Default(false),
		field.Text("brightness_schedules").Default("[]"),
		field.Int("brightness_override").Optional().Nillable(),
		field.Text("brightness_sensor_config").Optional().Nillable(),
		field.Bool("overlay_enabled").Default(false),
		field.String("overlay_position").Default("bottom").Validate(func(s string) error {
			switch s {
			case "top", "bottom":
				return nil
			default:
				return errors.New("overlay_position must be one of top, bottom")
			}
		}),
		field.Int("overlay_height").Default(8),
		field.String("overlay_text").Default(""),
		field.Int("overlay_speed_px").Default(0),
		field.String("overlay_bg").Default("#000000"),
		field.String("overlay_fg").Default("#ffffff"),
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
