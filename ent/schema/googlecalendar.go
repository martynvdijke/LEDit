package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// GoogleCalendar is a calendar datasource for private Google Calendar iCal
// feeds (https://calendar.google.com/calendar/ical/<id>/private-<hash>/basic.ics).
type GoogleCalendar struct {
	ent.Schema
}

func (GoogleCalendar) Fields() []ent.Field {
	return []ent.Field{
		field.String("url"),
		field.String("name").Default(""),
	}
}
