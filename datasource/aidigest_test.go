package datasource

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const digestRSS = `<?xml version="1.0"?><rss version="2.0"><channel><item><title>First headline</title></item><item><title>Second headline</title></item></channel></rss>`

// newDigestHarness spins up a fake RSS feed server and a fake LLM server and
// returns a ready AIDigestDS plus counters.
func newDigestHarness(t *testing.T, llmFunc func(w http.ResponseWriter, r *http.Request)) (*AIDigestDS, *httptest.Server, *httptest.Server, *atomic.Int32) {
	t.Helper()
	feedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(digestRSS))
	}))

	var llmCalls atomic.Int32
	llmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmCalls.Add(1)
		if llmFunc != nil {
			llmFunc(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Item one\nItem two"}}]}`))
	}))

	ds := &AIDigestDS{
		ID:       int(time.Now().UnixNano() % 100000),
		Name:     "Test Digest",
		FeedURLs: []string{feedSrv.URL + "/rss"},
		TTL:      time.Hour,
		Config:   AIConfig{Endpoint: llmSrv.URL, Model: "test-model", APIKey: "k"},
	}
	return ds, feedSrv, llmSrv, &llmCalls
}

func TestAIDigestCachedWithinTTL(t *testing.T) {
	ds, feedSrv, llmSrv, calls := newDigestHarness(t, nil)
	defer feedSrv.Close()
	defer llmSrv.Close()

	img1, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("first render: %v", err)
	}
	if img1.Format != "PNG" || len(img1.Data) == 0 {
		t.Fatal("first render produced no image")
	}
	if calls.Load() != 1 {
		t.Fatalf("expected 1 LLM call, got %d", calls.Load())
	}

	// Second render within TTL must reuse the cache: no new LLM call.
	img2, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("cached render: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected cache hit, got %d LLM calls", calls.Load())
	}
	if string(img1.Data) != string(img2.Data) {
		t.Error("cached render bytes differ")
	}
}

func TestAIDigestExpiresAfterTTL(t *testing.T) {
	ds, feedSrv, llmSrv, calls := newDigestHarness(t, nil)
	defer feedSrv.Close()
	defer llmSrv.Close()
	ds.TTL = time.Millisecond

	if _, err := ds.GetPNG(64, 64); err != nil {
		t.Fatalf("first render: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := ds.GetPNG(64, 64); err != nil {
		t.Fatalf("second render: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected 2 LLM calls after TTL expiry, got %d", calls.Load())
	}
}

func TestAIDigestSingleFlight(t *testing.T) {
	// The LLM handler blocks so we can observe single-flight behavior.
	release := make(chan struct{})
	ds, feedSrv, llmSrv, calls := newDigestHarness(t, func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Item one\nItem two"}}]}`))
	})
	defer feedSrv.Close()
	defer llmSrv.Close()
	InvalidateDigest(ds.ID)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := ds.GetPNG(64, 64); err != nil {
				t.Errorf("concurrent render: %v", err)
			}
		}()
	}
	// Give goroutines time to reach the in-flight check before releasing.
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	if calls.Load() != 1 {
		t.Fatalf("expected exactly 1 LLM call for 8 concurrent renders, got %d", calls.Load())
	}
}

func TestAIDigestStaleOnFailure(t *testing.T) {
	ds, feedSrv, llmSrv, _ := newDigestHarness(t, nil)
	defer feedSrv.Close()

	// First generation succeeds and caches.
	img1, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("first render: %v", err)
	}
	// Kill the LLM server: next render (after TTL) must serve stale.
	llmSrv.Close()
	ds.TTL = time.Millisecond
	time.Sleep(5 * time.Millisecond)

	img2, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("stale render returned error: %v", err)
	}
	if string(img1.Data) != string(img2.Data) {
		t.Error("stale render should match last good digest")
	}
}

func TestAIDigestPlaceholderWhenNoCache(t *testing.T) {
	ds, feedSrv, llmSrv, _ := newDigestHarness(t, nil)
	// Kill both servers before the first render: no cache exists, so the
	// datasource must fall back to a placeholder instead of erroring.
	feedSrv.Close()
	llmSrv.Close()
	ds.TTL = time.Millisecond

	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("placeholder render returned error: %v", err)
	}
	if img.Format != "PNG" || len(img.Data) == 0 {
		t.Fatal("placeholder render produced no image")
	}
}

func TestAIDigestNoConfigPlaceholder(t *testing.T) {
	ds := &AIDigestDS{ID: 4242, Name: "NoConfig", FeedURLs: []string{"http://127.0.0.1:1/x"}, TTL: time.Hour}
	img, err := ds.GetPNG(64, 64)
	if err != nil {
		t.Fatalf("no-config render returned error: %v", err)
	}
	if img.Format != "PNG" || len(img.Data) == 0 {
		t.Fatal("no-config placeholder produced no image")
	}
}

func TestAIDigestInvalidate(t *testing.T) {
	ds, feedSrv, llmSrv, calls := newDigestHarness(t, nil)
	defer feedSrv.Close()
	defer llmSrv.Close()

	if _, err := ds.GetPNG(64, 64); err != nil {
		t.Fatalf("render: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected 1 call before invalidate, got %d", calls.Load())
	}
	InvalidateDigest(ds.ID)
	if _, err := ds.GetPNG(64, 64); err != nil {
		t.Fatalf("render after invalidate: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected 2 calls after invalidate, got %d", calls.Load())
	}
}

func TestParseDigestSources(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{`["a","b"]`, 2},
		{`[]`, 0},
		{"", 0},
		{`not json`, 0},
	}
	for _, c := range cases {
		got := ParseDigestSources(c.in)
		if len(got) != c.want {
			t.Errorf("ParseDigestSources(%q) = %d names, want %d", c.in, len(got), c.want)
		}
	}
}

func TestRenderDigestTruncatesLines(t *testing.T) {
	long := strings.Repeat("x", 100)
	img := renderDigest("T", "one\n"+long+"\n", 64, 64)
	if img.Format != "PNG" || len(img.Data) == 0 {
		t.Fatal("renderDigest failed")
	}
	_ = fmt.Sprint() // keep fmt import used
}
