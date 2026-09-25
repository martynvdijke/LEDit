package schema

import (
	"errors"
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type DeviceMessageState struct {
	ent.Schema
}

func (DeviceMessageState) Fields() []ent.Field {
	return []ent.Field{
		field.Int("device_id"),
		field.String("message_id").NotEmpty(),
		field.String("status").Default("acked").Validate(func(s string) error {
			switch s {
			case "acked", "read", "dismissed":
				return nil
			default:
				return errors.New("status must be one of acked, read, dismissed")
			}
		}),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (DeviceMessageState) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("device_id", "message_id").Unique(),
	}
}
