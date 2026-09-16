package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// FirmwareSettings is the singleton OTA rollout policy. A per-device pin on
// DeviceSettings overrides both the channel target and the rollout cohort.
type FirmwareSettings struct {
	ent.Schema
}

func (FirmwareSettings) Fields() []ent.Field {
	return []ent.Field{
		field.String("channel").Default("stable"),
		field.String("target_version").Default(""),
		field.Int("rollout_percent").Default(0).Min(0).Max(100),
		field.Bool("paused").Default(false),
	}
}
