package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type Zigbee2MQTT struct {
	ent.Schema
}

func (Zigbee2MQTT) Fields() []ent.Field {
	return []ent.Field{
		field.String("token").Default("").Comment("Zigbee2MQTT API token"),
		field.String("url").Default("http://zigbee2mqtt:8080/api/devices").Comment("Zigbee2MQTT devices API URL"),
	}
}
