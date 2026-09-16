package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"ledit/ent"
	"ledit/ent/enttest"
	"ledit/ent/guestphoto"
)

func newGuestPhotoTestServer(t *testing.T) (*Server, *ent.Client) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dsn := fmt.Sprintf("file:%s?cache=shared&_fk=1&_busy_timeout=5000&mode=memory", strings.ReplaceAll(t.Name(), "/", "_"))
	drv, err := sql.Open(dialect.SQLite, dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	drv.DB().SetMaxOpenConns(1)
	client := enttest.NewClient(t, enttest.WithOptions(ent.Driver(drv)))
	t.Cleanup(func() { client.Close() })
	ctx := context.Background()
	if _, err := client.GeneralSettings.Create().SetTimeout(1).SetRandom(false).SetWidth(64).SetHeight(64).Save(ctx); err != nil {
		t.Fatalf("seed general: %v", err)
	}
	srv := &Server{DB: client, Ctx: ctx, Router: gin.New()}
	return srv, client
}

func seedGuestToken(t *testing.T, srv *Server, scopes []string) (*ent.GuestToken, string) {
	t.Helper()
	secret := fmt.Sprintf("secret-%d-%s", time.Now().UnixNano(), t.Name())
	h := sha256.Sum256([]byte(secret))
	hash := hex.EncodeToString(h[:])
	prefix := secret[:6]
	tok, err := srv.DB.GuestToken.Create().
		SetLabel("test").
		SetTokenHash(hash).
		SetTokenPrefix(prefix).
		SetScopes(scopes).
		SetCreatedAt(time.Now()).
		Save(srv.Ctx)
	if err != nil {
		t.Fatalf("create guest token: %v", err)
	}
	return tok, secret
}

func pngBytes() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	img.Set(1, 0, color.RGBA{0, 255, 0, 255})
	img.Set(0, 1, color.RGBA{0, 0, 255, 255})
	img.Set(1, 1, color.RGBA{255, 255, 0, 255})
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func multipartPhotoRequest(t *testing.T, uri, fieldName, fileName string, data []byte, secret string) *http.Request {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	fw, err := w.CreateFormFile(fieldName, fileName)
	if err != nil {
		t.Fatalf("create form: %v", err)
	}
	_, _ = fw.Write(data)
	w.Close()
	req := httptest.NewRequest(http.MethodPost, uri, &b)
	req.Header.Set("Content-Type", w.FormDataContentType())
	if secret != "" {
		req.Header.Set("X-Guest-Token", secret)
	}
	req.RemoteAddr = "127.0.0.1:1234"
	return req
}

func TestGuestPhotoUploadValid(t *testing.T) {
	srv, _ := newGuestPhotoTestServer(t)
	origDir := guestUploadDir
	tmp := t.TempDir()
	guestUploadDir = filepath.Join(tmp, "guest_uploads")
	t.Cleanup(func() { guestUploadDir = origDir })
	// clear rate buckets
	rateMu.Lock()
	rateBuckets = map[string][]time.Time{}
	rateMu.Unlock()

	tok, secret := seedGuestToken(t, srv, []string{"photo"})
	_ = tok

	data := pngBytes()
	req := multipartPhotoRequest(t, "/api/guest/photo", "photo", "test.png", data, secret)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set(ctxGuestToken, func() *ent.GuestToken {
		ct, _ := srv.DB.GuestToken.Query().All(srv.Ctx)
		return ct[0]
	}())
	// Instead use middleware simulation: set token directly
	// Use currentGuestToken path: we set via c.Set
	srv.APIGuestPhotoUpload(c)
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202 got %d %s", w.Code, w.Body.String())
	}
	// check row
	count, _ := srv.DB.GuestPhoto.Query().Count(srv.Ctx)
	if count != 1 {
		t.Fatalf("expected 1 row got %d", count)
	}
	gp, _ := srv.DB.GuestPhoto.Query().All(srv.Ctx)
	if gp[0].Status != guestphoto.StatusPending {
		t.Fatalf("status %s", gp[0].Status)
	}
	if _, err := os.Stat(gp[0].Path); err != nil {
		t.Fatalf("file not exists %v", err)
	}
}

func TestGuestPhotoTooLarge(t *testing.T) {
	srv, _ := newGuestPhotoTestServer(t)
	origDir := guestUploadDir
	tmp := t.TempDir()
	guestUploadDir = filepath.Join(tmp, "guest_uploads")
	t.Cleanup(func() { guestUploadDir = origDir })
	rateMu.Lock()
	rateBuckets = map[string][]time.Time{}
	rateMu.Unlock()

	_, secret := seedGuestToken(t, srv, []string{"photo"})
	// Create 6MiB payload but file header size is faked via multipart with large data
	big := make([]byte, 6*1024*1024)
	for i := range big {
		big[i] = byte(i)
	}
	req := multipartPhotoRequest(t, "/api/guest/photo", "photo", "big.png", big, secret)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	toks, _ := srv.DB.GuestToken.Query().All(srv.Ctx)
	c.Set(ctxGuestToken, toks[0])
	srv.APIGuestPhotoUpload(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "too_large") {
		t.Fatalf("missing too_large %s", w.Body.String())
	}
	cnt, _ := srv.DB.GuestPhoto.Query().Count(srv.Ctx)
	if cnt != 0 {
		t.Fatalf("row should not exist")
	}
}

func TestGuestPhotoInvalidImage(t *testing.T) {
	srv, _ := newGuestPhotoTestServer(t)
	origDir := guestUploadDir
	tmp := t.TempDir()
	guestUploadDir = filepath.Join(tmp, "guest_uploads")
	t.Cleanup(func() { guestUploadDir = origDir })
	rateMu.Lock()
	rateBuckets = map[string][]time.Time{}
	rateMu.Unlock()
	_, secret := seedGuestToken(t, srv, []string{"photo"})
	data := []byte("not an image")
	req := multipartPhotoRequest(t, "/api/guest/photo", "photo", "bad.png", data, secret)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	toks, _ := srv.DB.GuestToken.Query().All(srv.Ctx)
	c.Set(ctxGuestToken, toks[0])
	srv.APIGuestPhotoUpload(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid_image") {
		t.Fatalf("missing invalid_image %s", w.Body.String())
	}
	cnt, _ := srv.DB.GuestPhoto.Query().Count(srv.Ctx)
	if cnt != 0 {
		t.Fatalf("row should not exist")
	}
}

func TestGuestPhotoInsufficientScope(t *testing.T) {
	srv, _ := newGuestPhotoTestServer(t)
	_, secret := seedGuestToken(t, srv, []string{"message"})
	// Use middleware to check 403
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, r := gin.CreateTestContext(w)
	r.POST("/api/guest/photo", srv.GuestAuthMiddleware("photo"), srv.APIGuestPhotoUpload)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/guest/photo", nil)
	c.Request.Header.Set("X-Guest-Token", secret)
	r.ServeHTTP(w, c.Request)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 got %d %s", w.Code, w.Body.String())
	}
}

func TestGuestPhotoApproveReject(t *testing.T) {
	srv, _ := newGuestPhotoTestServer(t)
	origDir := guestUploadDir
	tmp := t.TempDir()
	guestUploadDir = filepath.Join(tmp, "guest_uploads")
	t.Cleanup(func() { guestUploadDir = origDir })

	// seed token and create pending photo
	tok, _ := seedGuestToken(t, srv, []string{"photo"})
	_ = os.MkdirAll(guestUploadDir, 0o755)
	path := filepath.Join(guestUploadDir, "approve.png")
	_ = os.WriteFile(path, pngBytes(), 0o644)
	gp := srv.DB.GuestPhoto.Create().SetPath(path).SetGuestTokenID(tok.ID).SetStatus(guestphoto.StatusPending).SetBytes(123).SetExpiresAt(time.Now().Add(time.Hour)).SaveX(srv.Ctx)

	// approve
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/guest/photo/%d/approve", gp.ID), nil)
	c.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", gp.ID)}}
	srv.APIGuestPhotoApprove(c)
	if w.Code != http.StatusOK {
		t.Fatalf("approve %d %s", w.Code, w.Body.String())
	}
	gp2, _ := srv.DB.GuestPhoto.Get(srv.Ctx, gp.ID)
	if gp2.Status != guestphoto.StatusApproved {
		t.Fatalf("not approved %s", gp2.Status)
	}
	if cnt, _ := srv.DB.Image.Query().Count(srv.Ctx); cnt != 1 {
		t.Fatalf("image not created %d", cnt)
	}
	// idempotent re-approve
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/guest/photo/%d/approve", gp.ID), nil)
	c2.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", gp.ID)}}
	srv.APIGuestPhotoApprove(c2)
	if w2.Code != http.StatusOK {
		t.Fatalf("re-approve %d", w2.Code)
	}

	// reject another
	path2 := filepath.Join(guestUploadDir, "reject.png")
	_ = os.WriteFile(path2, pngBytes(), 0o644)
	gp3 := srv.DB.GuestPhoto.Create().SetPath(path2).SetGuestTokenID(tok.ID).SetStatus(guestphoto.StatusPending).SetBytes(123).SetExpiresAt(time.Now().Add(time.Hour)).SaveX(srv.Ctx)
	w3 := httptest.NewRecorder()
	c3, _ := gin.CreateTestContext(w3)
	c3.Request = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/guest/photo/%d/reject", gp3.ID), nil)
	c3.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", gp3.ID)}}
	srv.APIGuestPhotoReject(c3)
	if w3.Code != http.StatusOK {
		t.Fatalf("reject %d %s", w3.Code, w3.Body.String())
	}
	gp3a, _ := srv.DB.GuestPhoto.Get(srv.Ctx, gp3.ID)
	if gp3a.Status != guestphoto.StatusRejected {
		t.Fatalf("not rejected %s", gp3a.Status)
	}
	if _, err := os.Stat(path2); !os.IsNotExist(err) {
		t.Fatalf("file not deleted")
	}
	// idempotent reject
	w4 := httptest.NewRecorder()
	c4, _ := gin.CreateTestContext(w4)
	c4.Request = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/guest/photo/%d/reject", gp3.ID), nil)
	c4.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", gp3.ID)}}
	srv.APIGuestPhotoReject(c4)
	if w4.Code != http.StatusOK {
		t.Fatalf("re-reject %d", w4.Code)
	}
}

func TestGuestPhotoPrune(t *testing.T) {
	srv, _ := newGuestPhotoTestServer(t)
	origDir := guestUploadDir
	tmp := t.TempDir()
	guestUploadDir = filepath.Join(tmp, "guest_uploads")
	t.Cleanup(func() { guestUploadDir = origDir })
	_ = os.MkdirAll(guestUploadDir, 0o755)
	tok, _ := seedGuestToken(t, srv, []string{"photo"})

	// expired row
	expPath := filepath.Join(guestUploadDir, "exp.png")
	_ = os.WriteFile(expPath, pngBytes(), 0o644)
	srv.DB.GuestPhoto.Create().SetPath(expPath).SetGuestTokenID(tok.ID).SetStatus(guestphoto.StatusPending).SetBytes(1).SetExpiresAt(time.Now().Add(-time.Hour)).SaveX(srv.Ctx)

	srv.pruneGuestPhotos(time.Now())
	if cnt, _ := srv.DB.GuestPhoto.Query().Count(srv.Ctx); cnt != 0 {
		t.Fatalf("expired not pruned %d", cnt)
	}
	if _, err := os.Stat(expPath); !os.IsNotExist(err) {
		t.Fatalf("expired file not deleted")
	}

	// cap: create 52 approved
	for i := 0; i < 52; i++ {
		p := filepath.Join(guestUploadDir, fmt.Sprintf("a%d.png", i))
		_ = os.WriteFile(p, pngBytes(), 0o644)
		srv.DB.GuestPhoto.Create().SetPath(p).SetGuestTokenID(tok.ID).SetStatus(guestphoto.StatusApproved).SetBytes(1).SetExpiresAt(time.Now().Add(time.Hour)).SaveX(srv.Ctx)
	}
	srv.pruneGuestPhotos(time.Now())
	cnt, _ := srv.DB.GuestPhoto.Query().Where(guestphoto.StatusEQ(guestphoto.StatusApproved)).Count(srv.Ctx)
	if cnt != 50 {
		t.Fatalf("expected 50 kept got %d", cnt)
	}
}

func TestGuestPhotoFlood(t *testing.T) {
	srv, _ := newGuestPhotoTestServer(t)
	origDir := guestUploadDir
	tmp := t.TempDir()
	guestUploadDir = filepath.Join(tmp, "guest_uploads")
	t.Cleanup(func() { guestUploadDir = origDir })
	rateMu.Lock()
	rateBuckets = map[string][]time.Time{}
	rateMu.Unlock()

	_, secret := seedGuestToken(t, srv, []string{"photo"})
	toks, _ := srv.DB.GuestToken.Query().All(srv.Ctx)
	tok := toks[0]
	data := pngBytes()
	var lastCode int
	var lastBody string
	var lastHeader string
	for i := 0; i < 6; i++ {
		req := multipartPhotoRequest(t, "/api/guest/photo", "photo", "f.png", data, secret)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set(ctxGuestToken, tok)
		srv.APIGuestPhotoUpload(c)
		lastCode = w.Code
		lastBody = w.Body.String()
		lastHeader = w.Header().Get("Retry-After")
		if i < 5 && w.Code != http.StatusAccepted {
			t.Fatalf("iter %d expected 202 got %d %s", i, w.Code, w.Body.String())
		}
	}
	if lastCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 got %d %s", lastCode, lastBody)
	}
	if lastHeader == "" {
		t.Fatalf("missing Retry-After")
	}
	_ = secret
}

func TestGuestPhotoMissingFile(t *testing.T) {
	srv, _ := newGuestPhotoTestServer(t)
	rateMu.Lock()
	rateBuckets = map[string][]time.Time{}
	rateMu.Unlock()
	toks, _ := srv.DB.GuestToken.Query().All(srv.Ctx)
	// need token
	tok, _ := seedGuestToken(t, srv, []string{"photo"})
	_ = toks
	req := httptest.NewRequest(http.MethodPost, "/api/guest/photo", nil)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=xxx")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set(ctxGuestToken, tok)
	srv.APIGuestPhotoUpload(c)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "missing_file") {
		t.Fatalf("expected missing_file got %d %s", w.Code, w.Body.String())
	}
}
