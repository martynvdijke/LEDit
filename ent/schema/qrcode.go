package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type Qrcode struct {
	ent.Schema
}

func (Qrcode) Fields() []ent.Field {
	return []ent.Field{
		field.String("content").MinLen(1).MaxLen(512),
		field.Enum("mode").Values("text", "url", "wifi").Default("text"),
		field.String("wifi_ssid").Default("").MaxLen(32),
		field.String("wifi_password").Default(""),
		field.Enum("wifi_auth").Values("WPA", "WEP", "nopass").Default("WPA"),
		field.String("caption").Default("").MaxLen(64),
		field.Enum("error_correction").Values("L", "M", "Q", "H").Default("M"),
		field.Int("quiet_zone").Default(4).Min(0).Max(8),
	}
}
