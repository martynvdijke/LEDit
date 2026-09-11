package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

// NowPlayingSource is a per-instance now-playing datasource backed by a media
// provider (Spotify/Plex/Jellyfin).
type NowPlayingSource struct {
	ent.Schema
}

func (NowPlayingSource) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").MinLen(1).MaxLen(64),
		field.Enum("provider").Values("spotify", "plex", "jellyfin").Default("jellyfin"),
		field.String("url").Default(""),
		field.String("token").Default(""),
		field.String("username").MaxLen(64).Default(""),
		field.Bool("show_album_art").Default(true),
	}
}
