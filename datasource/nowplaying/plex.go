package nowplaying

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// plexSession mirrors the attributes of a Plex /status/sessions <Track>.
type plexSession struct {
	Title            string `xml:"title,attr"`
	GrandparentTitle string `xml:"grandparentTitle,attr"`
	ParentTitle      string `xml:"parentTitle,attr"`
	Duration         int64  `xml:"duration,attr"`
	ViewOffset       int64  `xml:"viewOffset,attr"`
	Thumb            string `xml:"thumb,attr"`
	User             struct {
		Title string `xml:"title,attr"`
	} `xml:"User"`
}

type plexMediaContainer struct {
	Tracks []plexSession `xml:"Track"`
}

// ParsePlexSessions parses a Plex /status/sessions XML body into the shared
// NowPlaying model. The first <Track> is used, preferring one whose <User>
// matches usernameFilter when set. With no active track the result is a stop
// state and a nil error; malformed XML returns an error.
//
// ArtURL holds the raw Plex thumb path; the caller prefixes the server URL and
// X-Plex-Token.
func ParsePlexSessions(body []byte, usernameFilter string) (*NowPlaying, error) {
	var container plexMediaContainer
	if err := xml.Unmarshal(body, &container); err != nil {
		return nil, fmt.Errorf("parse plex: %w", err)
	}
	if len(container.Tracks) == 0 {
		return &NowPlaying{State: "stop"}, nil
	}
	candidate := &container.Tracks[0]
	if usernameFilter != "" {
		for i := range container.Tracks {
			if strings.EqualFold(container.Tracks[i].User.Title, usernameFilter) {
				candidate = &container.Tracks[i]
				break
			}
		}
	}
	np := &NowPlaying{State: "play"}
	np.Track = candidate.Title
	np.Artist = candidate.GrandparentTitle
	np.Album = candidate.ParentTitle
	np.Position = int(candidate.ViewOffset / 1000)
	np.Duration = int(candidate.Duration / 1000)
	np.ArtURL = candidate.Thumb
	return np, nil
}
