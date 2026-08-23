package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type TelegramSettings struct {
	ent.Schema
}

func (TelegramSettings) Fields() []ent.Field {
	return []ent.Field{
		field.Bool("enabled").Default(false),
		field.String("bot_token").Default(""),
		field.Int64("allowed_chat_id").Default(0),
	}
}
