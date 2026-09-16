package schema

import (
	"errors"
	"fmt"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
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
		field.Int("panel_cols").Default(1).Comment("Number of physical LED panels chained horizontally into this device's framebuffer"),
		field.Int("panel_gap").Default(0).Comment("Bezel gap in pixels between adjacent panels, hidden by the physical wall"),
		field.Bool("enabled").Default(true),
		field.String("token").Default(""),
		field.Int("refresh_interval").Default(60),
		field.Time("last_seen_at").Optional().Nillable(),
		field.Int("frames_served").Default(0),
		field.String("content_mode").Default("global").Validate(func(s string) error {
			switch s {
			case "global", "playlist", "scheduled":
				return nil
			default:
				return errors.New("content_mode must be one of global, playlist, scheduled")
			}
		}),
		field.Int("playlist_id").Optional().Nillable(),
		field.Text("scheduled_playlist_ids").Default("[]").Comment(`JSON: [1,2,3] ordered candidate playlist ids for scheduled mode`),
		field.Int("fallback_playlist_id").Optional().Nillable(),
		field.Bool("brightness_enabled").Default(false),
		field.Text("brightness_schedules").Default("[]").Comment(`JSON: [{days:[0..6], start:"HH:MM", end:"HH:MM", level:int}]`),
		field.Int("brightness_override").Optional().Nillable(),
		field.Text("brightness_sensor_config").Optional().Nillable().Comment(`JSON: {entity_id:string, lux_levels:[{maxLux,float level int}]}`),
		field.String("idle_screensaver").Optional().Nillable().Validate(func(s string) error {
			switch s {
			case "starfield", "dvd", "matrix", "plasma":
				return nil
			default:
				return errors.New("idle_screensaver must be one of starfield, dvd, matrix, plasma")
			}
		}),
		field.Int("group_id").Optional().Nillable(),
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
		field.String("transport").Default("websocket").Validate(func(s string) error {
			switch s {
			case "websocket", "wled", "artnet":
				return nil
			default:
				return fmt.Errorf("transport must be one of websocket, wled, artnet")
			}
		}),
		field.String("wled_host").Default(""),
		field.Int("wled_port").Default(4048),
		field.String("wled_realtime_mode").Default("ddp").Validate(func(s string) error {
			switch s {
			case "ddp", "e131", "http":
				return nil
			default:
				return fmt.Errorf("wled_realtime_mode must be one of ddp, e131, http")
			}
		}),
		field.Int("wled_channel").Default(0),
		field.String("artnet_host").Default(""),
		field.Int("artnet_port").Default(6454),
		field.Int("artnet_universe").Default(0),
		field.Int("output_fps").Default(20),
		field.String("output_color_order").Default("RGB").Validate(func(s string) error {
			switch s {
			case "RGB", "GRB", "BGR":
				return nil
			default:
				return fmt.Errorf("output_color_order must be one of RGB, GRB, BGR")
			}
		}),
		field.Float("output_gamma").Default(1.0),
		field.String("output_matrix_layout").Default("row-major").Validate(func(s string) error {
			switch s {
			case "row-major", "serpentine":
				return nil
			default:
				return fmt.Errorf("output_matrix_layout must be one of row-major, serpentine")
			}
		}),
		// discovery / enrollment
		field.String("fingerprint").Default(""),
		field.Time("approved_at").Optional().Nillable(),
		// firmware OTA
		field.String("firmware_version").Default(""),
		field.String("firmware_version_pin").Optional().Nillable(),
		field.String("last_update_status").Default(""),
		field.Time("last_update_at").Optional().Nillable(),
	}
}

func (DeviceSettings) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("group", DeviceGroup.Type).Ref("devices").Field("group_id").Unique().Annotations(entsql.OnDelete(entsql.SetNull)),
	}
}
