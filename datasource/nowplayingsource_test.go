package datasource

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNowPlayingSourceDSUnconfigured(t *testing.T) {
	ds := &NowPlayingSourceDS{Provider: "jellyfin"}
	img, err := ds.GetPNG(64, 64)
	if err != nil || img == nil {
		t.Fatalf("unconfigured: img=%v err=%v", img, err)
	}
}

func TestNowPlayingSourceDSIdle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()
	ds := &NowPlayingSourceDS{Provider: "jellyfin", URL: srv.URL, Token: "tok"}
	img, err := ds.GetPNG(64, 64)
	if err != nil || img == nil {
		t.Fatalf("idle: img=%v err=%v", img, err)
	}
}

func TestNowPlayingSourceDSAuthFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer srv.Close()
	ds := &NowPlayingSourceDS{Provider: "jellyfin", URL: srv.URL, Token: "bad"}
	if _, err := ds.GetPNG(64, 64); err == nil {
		t.Fatal("expected error on 401")
	}
}

func TestNowPlayingSourceDSPlexActive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<MediaContainer size="1"><Track title="Song" grandparentTitle="Artist" parentTitle="Album" duration="180000" viewOffset="60000"/></MediaContainer>`)
	}))
	defer srv.Close()
	ds := &NowPlayingSourceDS{Provider: "plex", URL: srv.URL, Token: "tok", ShowAlbumArt: false}
	img, err := ds.GetPNG(64, 64)
	if err != nil || img == nil {
		t.Fatalf("plex active: img=%v err=%v", img, err)
	}
}

func TestNowPlayingSourceDSJellyfinActive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"UserName":"alice","NowPlayingItem":{"Name":"Song","Artists":["Artist"],"Album":"Album","RunTimeTicks":2000000000,"Id":"abc","ImageTags":{"Primary":"tag"}},"PlayState":{"PositionTicks":1000000000,"IsPaused":false}}]`)
	}))
	defer srv.Close()
	ds := &NowPlayingSourceDS{Provider: "jellyfin", URL: srv.URL, Token: "tok", ShowAlbumArt: true}
	img, err := ds.GetPNG(64, 64)
	if err != nil || img == nil {
		t.Fatalf("jellyfin active: img=%v err=%v", img, err)
	}
}
