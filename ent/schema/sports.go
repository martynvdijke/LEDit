package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// Sports is a live/upcoming scores datasource. The legacy token field carries
// the provider API key, or an ESPN league slug for rows created before the
// provider/config fields existed (see datasource.SportsDS).
type Sports struct {
	ent.Schema
}

func (Sports) Fields() []ent.Field {
	return []ent.Field{
		field.String("token").Default("").Comment("Provider API key; legacy ESPN rows use it as the league slug"),
		field.String("url").Default("https://site.api.espn.com/apis/site/v2/sports/%s/scoreboard").Comment("Provider scoreboard API URL with %s for the league slug"),
		field.Enum("provider").Values("espn", "thesportsdb", "apifootball").Default("espn").Comment("Scores provider"),
		field.String("config").Default("").Comment("JSON {leagues,teams,fixtures,max_games,prefer_live}"),
		field.Int("live_refresh_seconds").Default(30).Min(15).Comment("Poll cadence while a followed game is live"),
		field.Int("idle_refresh_seconds").Default(300).Min(60).Comment("Poll cadence when no followed game is live"),
	}
}
