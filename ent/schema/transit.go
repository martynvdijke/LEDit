package schema

import (
	"fmt"
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// Transit is a departures board datasource for one stop. The legacy token
// field carries the stop id; url may contain %s to be substituted with it.
type Transit struct {
	ent.Schema
}

func (Transit) Fields() []ent.Field {
	return []ent.Field{
		field.String("token").Default("").Comment("Transit stop ID (legacy field name)"),
		field.String("url").Default("").Comment("Departures API URL; may contain %s for the stop ID; empty uses the provider default"),
		field.String("api_key").Default("").Sensitive().Comment("Provider API key; never logged"),
		field.Enum("provider").Values("vbb", "transitland", "511", "custom").Default("vbb"),
		field.Int("max_departures").Default(4).Min(1).Max(8),
		field.String("route_filter").Default("").MaxLen(256),
		field.Int("walk_time_min").Default(0).Min(0).Max(60),
		field.String("timezone").Default("Europe/Berlin").Validate(func(s string) error {
			if _, err := time.LoadLocation(s); err != nil {
				return fmt.Errorf("timezone must be a valid IANA location: %w", err)
			}
			return nil
		}),
		field.Enum("time_mode").Values("minutes", "clock").Default("minutes"),
	}
}
