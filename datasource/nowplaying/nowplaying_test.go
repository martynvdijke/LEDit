package nowplaying

import "testing"

func TestParsePlexSessionsActiveTrack(t *testing.T) {
	body := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<MediaContainer size="1">
  <Track title="Song" grandparentTitle="Artist" parentTitle="Album" duration="180000" viewOffset="60000" thumb="/library/metadata/1/thumb/2">
    <User id="1" title="alice"/>
  </Track>
</MediaContainer>`)
	np, err := ParsePlexSessions(body, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if np.Track != "Song" || np.Artist != "Artist" || np.Album != "Album" {
		t.Fatalf("bad metadata: %+v", np)
	}
	if np.Position != 60 || np.Duration != 180 {
		t.Fatalf("bad position/duration: %d/%d", np.Position, np.Duration)
	}
	if np.State != "play" {
		t.Fatalf("bad state: %s", np.State)
	}
	if np.ArtURL != "/library/metadata/1/thumb/2" {
		t.Fatalf("bad art: %q", np.ArtURL)
	}
}

func TestParsePlexSessionsUsernameFilter(t *testing.T) {
	body := []byte(`<MediaContainer size="2">
  <Track title="First" grandparentTitle="A"><User title="alice"/></Track>
  <Track title="Second" grandparentTitle="B"><User title="bob"/></Track>
</MediaContainer>`)
	np, err := ParsePlexSessions(body, "bob")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if np.Track != "Second" {
		t.Fatalf("filter not applied: %q", np.Track)
	}
}

func TestParsePlexSessionsNothingPlaying(t *testing.T) {
	np, err := ParsePlexSessions([]byte(`<MediaContainer size="0"></MediaContainer>`), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if np == nil || np.State != "stop" {
		t.Fatalf("expected stop, got %+v", np)
	}
}

func TestParsePlexSessionsMalformed(t *testing.T) {
	if _, err := ParsePlexSessions([]byte(`<MediaContainer><Track`), ""); err == nil {
		t.Fatal("expected error for malformed xml")
	}
}

func TestParsePlexSessionsMissingOptionalFields(t *testing.T) {
	np, err := ParsePlexSessions([]byte(`<MediaContainer size="1"><Track title="Only"/></MediaContainer>`), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if np.Track != "Only" || np.Artist != "" || np.Album != "" || np.Duration != 0 {
		t.Fatalf("unexpected: %+v", np)
	}
}

func TestParseSpotifyNowPlayingActive(t *testing.T) {
	body := []byte(`{
      "is_playing": true,
      "progress_ms": 65000,
      "item": {
        "name": "Track",
        "duration_ms": 200000,
        "artists": [{"name": "Artist"}, {"name": "Other"}],
        "album": {
          "name": "Album",
          "images": [
            {"url": "small.jpg", "width": 64, "height": 64},
            {"url": "big.jpg", "width": 640, "height": 640}
          ]
        }
      }
    }`)
	np, err := ParseSpotifyNowPlaying(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if np.Track != "Track" || np.Artist != "Artist" || np.Album != "Album" {
		t.Fatalf("bad metadata: %+v", np)
	}
	if np.Position != 65 || np.Duration != 200 {
		t.Fatalf("bad position/duration: %d/%d", np.Position, np.Duration)
	}
	if np.State != "play" {
		t.Fatalf("bad state: %s", np.State)
	}
	if np.ArtURL != "big.jpg" {
		t.Fatalf("expected largest image, got %q", np.ArtURL)
	}
}

func TestParseSpotifyNowPlayingPaused(t *testing.T) {
	np, err := ParseSpotifyNowPlaying([]byte(`{"is_playing":false,"item":{"name":"T"}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if np.State != "pause" || np.Track != "T" {
		t.Fatalf("unexpected: %+v", np)
	}
}

func TestParseSpotifyNowPlayingNothingPlaying(t *testing.T) {
	for _, body := range [][]byte{nil, []byte(""), []byte("  \n"), []byte(`{"is_playing":false}`)} {
		np, err := ParseSpotifyNowPlaying(body)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", body, err)
		}
		if np == nil || np.State != "stop" {
			t.Fatalf("expected stop for %q, got %+v", body, np)
		}
	}
}

func TestParseSpotifyNowPlayingMalformed(t *testing.T) {
	if _, err := ParseSpotifyNowPlaying([]byte(`{"item":`)); err == nil {
		t.Fatal("expected error for malformed json")
	}
}

func TestParseSpotifyNowPlayingMissingOptionalFields(t *testing.T) {
	np, err := ParseSpotifyNowPlaying([]byte(`{"is_playing":true,"item":{"name":"Solo"}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if np.Track != "Solo" || np.Artist != "" || np.Album != "" || np.ArtURL != "" {
		t.Fatalf("unexpected: %+v", np)
	}
}
