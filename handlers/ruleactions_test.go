package handlers

import "testing"

func TestParseThenAction(t *testing.T) {
	cases := []struct {
		raw     string
		kind    string
		wantErr bool
	}{
		{"", "none", false},
		{"{}", "none", false},
		{`{"kind":"none"}`, "none", false},
		{`{"kind":"scene","scene_id":5}`, "scene", false},
		{`{"kind":"unknown"}`, "", true},
	}
	for _, c := range cases {
		ta, err := ParseThenAction(c.raw)
		if c.wantErr && err == nil {
			t.Fatalf("ParseThenAction %q expected error", c.raw)
		}
		if !c.wantErr && err != nil {
			t.Fatalf("ParseThenAction %q unexpected error: %v", c.raw, err)
		}
		if !c.wantErr && ta.Kind != c.kind {
			t.Fatalf("ParseThenAction %q kind=%q want %q", c.raw, ta.Kind, c.kind)
		}
	}
}

func TestValidateThenAction(t *testing.T) {
	if msg := validateThenAction(ThenAction{Kind: "scene"}); msg == "" {
		t.Fatal("scene without id should fail")
	}
	if msg := validateThenAction(ThenAction{Kind: "scene", SceneID: 1}); msg != "" {
		t.Fatalf("scene with id should pass, got %q", msg)
	}
	if msg := validateThenAction(ThenAction{Kind: "notification"}); msg == "" {
		t.Fatal("notification without message should fail")
	}
	if msg := validateThenAction(ThenAction{Kind: "notification", Message: "hi"}); msg != "" {
		t.Fatalf("notification with message should pass, got %q", msg)
	}
	if msg := validateThenAction(ThenAction{Kind: "notification", Message: "   "}); msg == "" {
		t.Fatal("notification whitespace message should fail")
	}
}
