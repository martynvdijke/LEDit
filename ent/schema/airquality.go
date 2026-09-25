package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type AirQuality struct {
	ent.Schema
}

func (AirQuality) Fields() []ent.Field {
	return []ent.Field{
		field.String("token").Default("").Comment("OpenWeather/AirQuality API key"),
		field.String("url").Default("").Comment("Air quality API URL (OWM /air_pollution or OpenAQ)"),
	}
}
