package schema

import (
	"errors"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

type GeneralSettings struct {
	ent.Schema
}

func (GeneralSettings) Fields() []ent.Field {
	return []ent.Field{
		field.Float("timeout"),
		field.Bool("random"),
		field.Int("width").Default(64),
		field.Int("height").Default(64),
		field.Text("theme").Default("{}").Optional(),
		field.Bool("eink_mode").Default(false).Optional(),
		field.String("transition_style").Default("none").Validate(func(s string) error {
			switch s {
			case "none", "fade", "wipe", "dissolve":
				return nil
			default:
				return errors.New("transition_style must be one of none, fade, wipe, dissolve")
			}
		}),
		field.Int("transition_ms").Default(500).Min(100).Max(2000),
		field.Int("chart_retention_hours").Default(48),
		field.Int("chart_max_points_per_source").Default(576),
		field.String("now_playing_provider").Default("disabled"),
		field.String("ordering_mode").Default("random").Validate(func(s string) error {
			switch s {
			case "sequential", "random", "adaptive":
				return nil
			default:
				return errors.New("ordering_mode must be one of sequential, random, adaptive")
			}
		}),
		field.Float("adaptive_floor").Default(0.05),
		field.Int("adaptive_half_life_days").Default(7),
		field.Int("adaptive_window_days").Default(14),
		field.Float("adaptive_epsilon").Default(0.15),
	}
}

func (GeneralSettings) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("sonarr", Sonarr.Type),
		edge.To("radarr", Radarr.Type),
		edge.To("f1", F1.Type),
		edge.To("weather", Weather.Type),
		edge.To("home_assistant", HomeAssistant.Type),
		edge.To("untappd", Untappd.Type),
		edge.To("images", Image.Type),
		edge.To("videos", Video.Type),
		edge.To("crypto", Crypto.Type),
		edge.To("schedules", Schedule.Type),
		edge.To("device_settings", DeviceSettings.Type),
		edge.To("rss_feeds", RssFeed.Type),
		edge.To("calendars", Calendar.Type),
		edge.To("stocks", Stock.Type),
		edge.To("text_slides", TextSlide.Type),
		edge.To("email_settings", EmailSettings.Type),
		edge.To("ai_settings", AISettings.Type),
		edge.To("umami_settings", UmamiSettings.Type),
		edge.To("google_calendars", GoogleCalendar.Type),
		edge.To("news_feeds", NewsFeed.Type),
		edge.To("generic_apis", GenericAPI.Type),
		edge.To("matrix_layouts", MatrixLayout.Type),
		edge.To("countdowns", Countdown.Type),
		edge.To("ai_digests", AIDigest.Type),
		edge.To("alert_settings", AlertSettings.Type),
		edge.To("pixel_arts", PixelArt.Type),
		edge.To("playlists", Playlist.Type),
		edge.To("displayrules", DisplayRule.Type),
		edge.To("webhooksettings", WebhookSettings.Type),
		edge.To("mqttsettings", MQTTSettings.Type),
		edge.To("telegramsettings", TelegramSettings.Type),
		edge.To("transits", Transit.Type),
		edge.To("uptimes", Uptime.Type),
		edge.To("piholes", PiHole.Type),
		edge.To("githubs", GitHub.Type),
		edge.To("sports", Sports.Type),
		edge.To("sunmoons", SunMoon.Type),
		edge.To("jellyfins", Jellyfin.Type),
		edge.To("mpds", MPD.Type),
		edge.To("qrcodes", Qrcode.Type),
	}
}
