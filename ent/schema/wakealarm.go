package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// WakeAlarm is a recurring scheduled takeover: at `start` on a listed weekday
// the feed holds the resolved wake source until `end` (exclusive), optionally
// ramping brightness. Days is a JSON array of weekday integers (0=Sunday).
type WakeAlarm struct {
	ent.Schema
}

func (WakeAlarm) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty(),
		field.Bool("enabled").Default(true),
		field.Text("days").Default("[]").Comment(`JSON: [1,2,3,4,5] (0=Sunday)`),
		field.String("start").Default("00:00"),
		field.String("end").Default("00:00"),
		field.String("wake_source_type").Default(""),
		field.Int("wake_source_id").Default(0),
		field.Bool("brightness_enabled").Default(false),
		field.Int("brightness_start").Default(0).Min(0).Max(100),
		field.Int("brightness_end").Default(100).Min(0).Max(100),
		field.Int("brightness_ramp_seconds").Default(600).Min(0).Max(3600),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
