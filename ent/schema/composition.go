package schema

import (
	"errors"
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// Composition is a dashboard layout that composites N child sources into
// non-uniform regions of one frame. Regions is a JSON array of region objects
// (a grid cell or an absolute rectangle) referencing sources by type and DB id.
type Composition struct {
	ent.Schema
}

func (Composition) Fields() []ent.Field {
	return []ent.Field{
		field.String("name"),
		field.Bool("enabled").Default(true),
		field.String("mode").Default("grid").Validate(func(s string) error {
			switch s {
			case "grid", "absolute":
				return nil
			default:
				return errors.New("mode must be one of grid, absolute")
			}
		}),
		field.Int("rows").Default(1),
		field.Int("cols").Default(1),
		field.Int("gap").Default(0),
		field.Int("padding").Default(0),
		field.String("background").Default("#282a36"),
		field.Text("regions").Default("[]").Comment("JSON array of region objects"),
		field.Int("ttl_seconds").Default(0),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
