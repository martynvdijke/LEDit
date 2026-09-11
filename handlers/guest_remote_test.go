package handlers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

func newGuestRemoteTestServer(t *testing.T) *Server {
	t.Helper()
	dsn := fmt.Sprintf("file:guestremote_%d.db?cache=shared&_fk=1&_busy_timeout=5000&mode=memory", time.Now().UnixNano())
	drv, err := sql.Open(dialect.SQLite, dsn)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() { drv.Close() })
	return New(drv, nil)
}

func createGuestToken(t *testing.T, srv *Server, scopes []string, expiresAt, revokedAt *time.Time) string {
	t.Helper()
	secret, prefix := generateGuestToken()
	b := srv.DB.GuestToken.Create().
		SetTokenHash(hashGuestToken(secret)).
		SetTokenPrefix(prefix).
		SetScopes(scopes).
		SetCreatedAt(time.Now())
	if expiresAt != nil {
		b.SetExpiresAt(*expiresAt)
	}
	if revokedAt != nil {
		b.SetRevokedAt(*revokedAt)
	}
	b.SaveX(srv.Ctx)
	return secret
}

// guestTestRouter mounts a single scope-protected probe route.
func guestTestRouter(srv *Server, scope string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/g/action", srv.GuestAuthMiddleware(scope), func(c *gin.Context) {
		if currentGuestToken(c) == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "no token in context"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	return r
}

func guestReq(r *gin.Engine, secret, bearer string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/g/action", nil)
	if secret != "" {
		req.Header.Set("X-Guest-Token", secret)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ---------------------------------------------------------------------------
// 2.4 Middleware acceptance/rejection matrix
// ---------------------------------------------------------------------------

func TestGuestAuthValidTokenAccepted(t *testing.T) {
	srv := newGuestRemoteTestServer(t)
	secret := createGuestToken(t, srv, []string{"next"}, nil, nil)
	if w := guestReq(guestTestRouter(srv, "next"), secret, ""); w.Code != http.StatusOK {
		t.Fatalf("valid token: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestGuestAuthBearerGuestTokenAccepted(t *testing.T) {
	srv := newGuestRemoteTestServer(t)
	secret := createGuestToken(t, srv, []string{"next"}, nil, nil)
	if w := guestReq(guestTestRouter(srv, "next"), "", secret); w.Code != http.StatusOK {
		t.Fatalf("bearer guest token: expected 200, got %d", w.Code)
	}
}

func TestGuestAuthUnknownToken401(t *testing.T) {
	srv := newGuestRemoteTestServer(t)
	if w := guestReq(guestTestRouter(srv, "next"), "not-a-real-token", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("unknown token: expected 401, got %d", w.Code)
	}
}

func TestGuestAuthRevoked401(t *testing.T) {
	srv := newGuestRemoteTestServer(t)
	now := time.Now()
	secret := createGuestToken(t, srv, []string{"next"}, nil, &now)
	if w := guestReq(guestTestRouter(srv, "next"), secret, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token: expected 401, got %d", w.Code)
	}
}

func TestGuestAuthExpired401(t *testing.T) {
	srv := newGuestRemoteTestServer(t)
	past := time.Now().Add(-time.Hour)
	secret := createGuestToken(t, srv, []string{"next"}, &past, nil)
	if w := guestReq(guestTestRouter(srv, "next"), secret, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("expired token: expected 401, got %d", w.Code)
	}
}

func TestGuestAuthMissingScope403(t *testing.T) {
	srv := newGuestRemoteTestServer(t)
	secret := createGuestToken(t, srv, []string{"pause", "next"}, nil, nil)
	w := guestReq(guestTestRouter(srv, "message"), secret, "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("out-of-scope: expected 403, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "insufficient_scope") {
		t.Fatalf("expected insufficient_scope code, got %s", w.Body.String())
	}
}

func TestGuestAuthMissingHeader401(t *testing.T) {
	srv := newGuestRemoteTestServer(t)
	if w := guestReq(guestTestRouter(srv, "next"), "", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("missing header: expected 401, got %d", w.Code)
	}
}

func TestGuestAuthSessionCookieAlone401(t *testing.T) {
	srv := newGuestRemoteTestServer(t)
	session := loginAsAdmin(t, srv)
	req := httptest.NewRequest(http.MethodPost, "/g/action", nil)
	req.AddCookie(session)
	w := httptest.NewRecorder()
	guestTestRouter(srv, "next").ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("admin session cookie alone: expected 401, got %d", w.Code)
	}
}

func TestGuestAuthAPITokenBearer401(t *testing.T) {
	srv := newGuestRemoteTestServer(t)
	owner := srv.DB.AdminSettings.Query().FirstX(srv.Ctx)
	apiSecret := createTestToken(t, srv, owner.ID, nil, nil)
	if w := guestReq(guestTestRouter(srv, "next"), "", apiSecret); w.Code != http.StatusUnauthorized {
		t.Fatalf("admin API token bearer: expected 401, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// 2.3 Limiter window rollover and per-key isolation
// ---------------------------------------------------------------------------

func TestGuestRateLimitWindowRollover(t *testing.T) {
	key := "test:rollover:" + t.Name()
	for i := 0; i < 3; i++ {
		if ok, _ := checkGuestRateLimit(key, 3); !ok {
			t.Fatalf("request %d within limit should be allowed", i+1)
		}
	}
	if ok, retry := checkGuestRateLimit(key, 3); ok || retry < 1 {
		t.Fatalf("4th request should be throttled with positive retry, got ok=%v retry=%d", ok, retry)
	}
	// Age the recorded hits out of the one-minute window.
	guestRateMu.Lock()
	old := time.Now().Add(-2 * time.Minute)
	for i := range guestRate[key] {
		guestRate[key][i] = old
	}
	guestRateMu.Unlock()
	if ok, _ := checkGuestRateLimit(key, 3); !ok {
		t.Fatal("after window rollover a new request should be allowed")
	}
}

func TestGuestRateLimitPerKeyIsolation(t *testing.T) {
	a := "test:isolation:a:" + t.Name()
	b := "test:isolation:b:" + t.Name()
	for i := 0; i < 2; i++ {
		checkGuestRateLimit(a, 2)
	}
	if ok, _ := checkGuestRateLimit(a, 2); ok {
		t.Fatal("key a should be throttled")
	}
	if ok, _ := checkGuestRateLimit(b, 2); !ok {
		t.Fatal("key b should not be affected by key a's usage")
	}
}
