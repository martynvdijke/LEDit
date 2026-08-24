package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type SunMoon struct {
	ent.Schema
}

func (SunMoon) Fields() []ent.Field {
	return []ent.Field{
		field.String("token").Default("").Comment("Coordinates as lat,lng"),
		field.String("url").Default("https://api.sunrise-sunset.org/json?lat=%s&lng=%s&formatted=0").Comment("Sunrise-sunset API URL with %s for lat and %s for lng"),
	}
}
