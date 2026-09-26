package datasource

import (
	"strings"
	"testing"
)

func TestUntappdBuildURL(t *testing.T) {
	tests := []struct {
		name  string
		ds    UntappdDS
		check func(t *testing.T, url string)
	}{
		{
			name: "client_id secret",
			ds:   UntappdDS{Token: "cid/secret"},
			check: func(t *testing.T, url string) {
				if !strings.Contains(url, "client_id=cid") || !strings.Contains(url, "client_secret=secret") {
					t.Fatalf("url %q", url)
				}
			},
		},
		{
			name: "access_token",
			ds:   UntappdDS{Token: "accesstoken"},
			check: func(t *testing.T, url string) {
				if !strings.Contains(url, "access_token=accesstoken") {
					t.Fatalf("url %q", url)
				}
			},
		},
		{
			name: "custom with %s",
			ds:   UntappdDS{Token: "tok", URL: "https://example.com/%s/checkins"},
			check: func(t *testing.T, url string) {
				if url != "https://example.com/tok/checkins" {
					t.Fatalf("url %q", url)
				}
			},
		},
		{
			name: "custom verbatim",
			ds:   UntappdDS{Token: "tok", URL: "https://example.com/fixed"},
			check: func(t *testing.T, url string) {
				if url != "https://example.com/fixed" {
					t.Fatalf("url %q", url)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := tt.ds.buildURL()
			tt.check(t, u)
		})
	}
}

func TestParseUntappdResponse(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
		check   func(t *testing.T, m map[string]string)
	}{
		{
			name: "checkins items",
			body: `{"response":{"checkins":{"items":[{"rating_score":4.5,"beer":{"beer_name":"IPA","beer_abv":6.5,"beer_style":"IPA"},"brewery":{"brewery_name":"Brew Co"}}]}}}`,
			check: func(t *testing.T, m map[string]string) {
				if m["brewery"] != "Brew Co" || m["beer"] != "IPA" {
					t.Fatalf("got %v", m)
				}
				if _, ok := m["abv"]; !ok {
					t.Fatalf("abv missing %v", m)
				}
				if _, ok := m["rating"]; !ok {
					t.Fatalf("rating missing %v", m)
				}
			},
		},
		{
			name: "response checkin",
			body: `{"response":{"checkin":{"rating_score":3.0,"beer":{"beer_name":"Lager","beer_abv":5.0},"brewery":{"brewery_name":"Brew2"}}}}`,
			check: func(t *testing.T, m map[string]string) {
				if m["beer"] != "Lager" {
					t.Fatalf("got %v", m)
				}
			},
		},
		{
			name: "beers items",
			body: `{"response":{"beers":{"items":[{"beer":{"beer_name":"Stout","beer_abv":8.0},"brewery":{"brewery_name":"Dark Brew"},"rating_score":4.0}]}}}`,
			check: func(t *testing.T, m map[string]string) {
				if m["beer"] != "Stout" {
					t.Fatalf("got %v", m)
				}
			},
		},
		{name: "empty response", body: `{"response":{}}`, wantErr: true},
		{name: "invalid JSON", body: `not json`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := parseUntappdResponse([]byte(tt.body))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err %v", err)
			}
			if tt.check != nil {
				tt.check(t, m)
			}
		})
	}
}

func TestUntappdGetPNGFallback(t *testing.T) {
	ds := &UntappdDS{Token: "", URL: ""}
	img, err := ds.GetPNG(64, 32)
	if err != nil {
		t.Fatalf("GetPNG: %v", err)
	}
	if img == nil || img.Data == nil {
		t.Fatal("expected image")
	}
}
