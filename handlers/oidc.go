package handlers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"
	"ledit/ent/user"
)

// OIDC config from env. Secrets via OIDC_CLIENT_SECRET_FILE (preferred) or
// OIDC_CLIENT_SECRET env. Never committed to git.
func oidcEnabled() bool {
	v := os.Getenv("OIDC_ENABLED")
	return v == "true" || v == "1"
}

func oidcSecret() string {
	if f := os.Getenv("OIDC_CLIENT_SECRET_FILE"); f != "" {
		if b, err := os.ReadFile(f); err == nil {
			if s := strings.TrimSpace(string(b)); s != "" {
				return s
			}
		}
	}
	return strings.TrimSpace(os.Getenv("OIDC_CLIENT_SECRET"))
}

func oidcScopes() []string {
	raw := os.Getenv("OIDC_SCOPES")
	if raw == "" {
		return []string{"openid", "email", "profile", "groups"}
	}
	return strings.Fields(strings.ReplaceAll(raw, ",", " "))
}

func oidcOAuthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     os.Getenv("OIDC_CLIENT_ID"),
		ClientSecret: oidcSecret(),
		RedirectURL:  os.Getenv("OIDC_REDIRECT_URL"),
		Scopes:       oidcScopes(),
		Endpoint: oauth2.Endpoint{
			// Discovered at login time; placeholder replaced in OIDCLogin.
			AuthURL:  strings.TrimSuffix(os.Getenv("OIDC_ISSUER_URL"), "/") + "/api/oidc/authorization",
			TokenURL: strings.TrimSuffix(os.Getenv("OIDC_ISSUER_URL"), "/") + "/api/oidc/token",
		},
	}
}

var (
	oidcMu       sync.Mutex
	oidcVerifier *oidc.IDTokenVerifier
	oidcIssuer   string
	oidcProvider *oidc.Provider
)

// oidcVerifierFor returns an ID token verifier discovered from the issuer.
func oidcVerifierFor(ctx context.Context) (*oidc.IDTokenVerifier, error) {
	issuer := strings.TrimSuffix(os.Getenv("OIDC_ISSUER_URL"), "/")
	oidcMu.Lock()
	defer oidcMu.Unlock()
	if oidcVerifier != nil && oidcIssuer == issuer {
		return oidcVerifier, nil
	}
	p, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, err
	}
	oidcProvider = p
	oidcIssuer = issuer
	oidcVerifier = p.Verifier(&oidc.Config{ClientID: os.Getenv("OIDC_CLIENT_ID")})
	return oidcVerifier, nil
}

func oidcRand(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func pkceChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

func oidcCookieSecure() bool {
	return strings.HasPrefix(os.Getenv("OIDC_REDIRECT_URL"), "https://")
}

// OIDCLogin starts Authorization Code + PKCE S256 flow.
func (s *Server) OIDCLogin(c *gin.Context) {
	if !oidcEnabled() {
		c.JSON(http.StatusNotFound, gin.H{"error": "oidc_disabled"})
		return
	}
	issuer := strings.TrimSuffix(os.Getenv("OIDC_ISSUER_URL"), "/")
	ctx := c.Request.Context()
	p, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		c.HTML(http.StatusOK, "login.html", gin.H{"error": "OIDC discovery failed", "OIDCEnabled": true})
		return
	}
	oidcMu.Lock()
	oidcProvider = p
	oidcIssuer = issuer
	oidcVerifier = p.Verifier(&oidc.Config{ClientID: os.Getenv("OIDC_CLIENT_ID")})
	oidcMu.Unlock()

	state := oidcRand(24)
	nonce := oidcRand(24)
	verifier := oidcRand(32)
	secure := oidcCookieSecure()
	c.SetCookie("oidc_state", state, 600, "/", "", secure, true)
	c.SetCookie("oidc_nonce", nonce, 600, "/", "", secure, true)
	c.SetCookie("oidc_verifier", verifier, 600, "/", "", secure, true)

	cfg := oidcOAuthConfig()
	cfg.Endpoint = p.Endpoint()
	cfg.Endpoint.AuthStyle = oauth2.AuthStyleInParams // client_secret_post per Authelia template
	authURL := cfg.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier))
	c.Redirect(http.StatusFound, authURL)
}

type oidcClaims struct {
	Sub           string   `json:"sub"`
	Email         string   `json:"email"`
	EmailVerified bool     `json:"email_verified"`
	Groups        []string `json:"groups"`
	PreferredName string   `json:"preferred_username"`
	Name          string   `json:"name"`
	Nonce         string   `json:"nonce"`
}

// OIDCCallback verifies state/nonce/PKCE + ID token, links/provisions user.
func (s *Server) OIDCCallback(c *gin.Context) {
	if !oidcEnabled() {
		c.JSON(http.StatusNotFound, gin.H{"error": "oidc_disabled"})
		return
	}
	stateQ := c.Query("state")
	stateC, err := c.Cookie("oidc_state")
	if err != nil || stateQ == "" || stateQ != stateC {
		c.HTML(http.StatusOK, "login.html", gin.H{"error": "Invalid OIDC state", "OIDCEnabled": true})
		return
	}
	nonceC, err := c.Cookie("oidc_nonce")
	if err != nil {
		c.HTML(http.StatusOK, "login.html", gin.H{"error": "Missing OIDC nonce", "OIDCEnabled": true})
		return
	}
	verifierC, err := c.Cookie("oidc_verifier")
	if err != nil {
		c.HTML(http.StatusOK, "login.html", gin.H{"error": "Missing PKCE verifier", "OIDCEnabled": true})
		return
	}
	secure := oidcCookieSecure()
	for _, k := range []string{"oidc_state", "oidc_nonce", "oidc_verifier"} {
		c.SetCookie(k, "", -1, "/", "", secure, true)
	}

	ctx := c.Request.Context()
	verifier, err := oidcVerifierFor(ctx)
	if err != nil || oidcProvider == nil {
		c.HTML(http.StatusOK, "login.html", gin.H{"error": "OIDC discovery failed", "OIDCEnabled": true})
		return
	}
	cfg := oidcOAuthConfig()
	cfg.Endpoint = oidcProvider.Endpoint()
	cfg.Endpoint.AuthStyle = oauth2.AuthStyleInParams
	tok, err := cfg.Exchange(ctx, c.Query("code"), oauth2.VerifierOption(verifierC))
	if err != nil {
		c.HTML(http.StatusOK, "login.html", gin.H{"error": "OIDC code exchange failed", "OIDCEnabled": true})
		return
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok || rawID == "" {
		c.HTML(http.StatusOK, "login.html", gin.H{"error": "Missing ID token", "OIDCEnabled": true})
		return
	}
	idTok, err := verifier.Verify(ctx, rawID)
	if err != nil {
		c.HTML(http.StatusOK, "login.html", gin.H{"error": "Invalid ID token", "OIDCEnabled": true})
		return
	}
	var cl oidcClaims
	if err := idTok.Claims(&cl); err != nil {
		c.HTML(http.StatusOK, "login.html", gin.H{"error": "Invalid ID token claims", "OIDCEnabled": true})
		return
	}
	if cl.Nonce != nonceC {
		c.HTML(http.StatusOK, "login.html", gin.H{"error": "Invalid OIDC nonce", "OIDCEnabled": true})
		return
	}
	if !cl.EmailVerified || cl.Email == "" {
		c.HTML(http.StatusOK, "login.html", gin.H{"error": "Verified email required", "OIDCEnabled": true})
		return
	}
	email := strings.ToLower(strings.TrimSpace(cl.Email))
	role := "viewer"
	for _, g := range cl.Groups {
		if g == "admins" {
			role = "admin"
			break
		}
	}

	// Link by oidc_sub, then by verified email, else auto-provision.
	u, err := s.DB.User.Query().Where(user.OidcSubEQ(cl.Sub)).Only(s.Ctx)
	if err != nil {
		u, err = s.DB.User.Query().Where(user.EmailEQ(email)).Only(s.Ctx)
		if err == nil {
			u, err = s.DB.User.UpdateOne(u).SetOidcSub(cl.Sub).SetAuthMethod(user.AuthMethodOidc).SetRole(user.Role(role)).SetLastLoginAt(time.Now()).Save(s.Ctx)
		}
	} else {
		u, err = s.DB.User.UpdateOne(u).SetAuthMethod(user.AuthMethodOidc).SetRole(user.Role(role)).SetEmail(email).SetLastLoginAt(time.Now()).Save(s.Ctx)
	}
	if err != nil || u == nil {
		username := oidcUsername(email, cl.PreferredName, cl.Name)
		// Unusable random password so OIDC users can't password-login.
		hash, _ := bcrypt.GenerateFromPassword([]byte(oidcRand(32)), bcrypt.DefaultCost)
		create := s.DB.User.Create().
			SetUsername(username).
			SetPasswordHash(string(hash)).
			SetRole(user.Role(role)).
			SetEmail(email).
			SetOidcSub(cl.Sub).
			SetAuthMethod(user.AuthMethodOidc).
			SetCreatedAt(time.Now()).
			SetLastLoginAt(time.Now())
		u, err = create.Save(s.Ctx)
		if err != nil {
			c.HTML(http.StatusOK, "login.html", gin.H{"error": "Failed to provision user", "OIDCEnabled": true})
			return
		}
	}

	token := hashSessionToken(time.Now().String() + email)
	authMu.Lock()
	sessions[token] = sessionData{UserID: u.ID, Username: u.Username, Role: string(u.Role), Expiry: time.Now().Add(24 * time.Hour)}
	authMu.Unlock()
	c.SetCookie("session", token, 86400, "/", "", false, true)
	c.Redirect(http.StatusFound, "/admin/")
}

// oidcUsername derives a valid unique username from email/name claims.
func oidcUsername(email, preferred, name string) string {
	candidates := []string{preferred, name, strings.Split(email, "@")[0]}
	var base string
	for _, cand := range candidates {
		sanitized := strings.Map(func(r rune) rune {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-', r == ' ':
				return r
			default:
				return '-'
			}
		}, strings.TrimSpace(cand))
		sanitized = strings.ReplaceAll(strings.Trim(sanitized, "-. "), " ", "-")
		if len(sanitized) >= 3 {
			base = sanitized[:min(64, len(sanitized))]
			break
		}
	}
	if base == "" {
		base = "oidc-user"
	}
	return base
}

// OIDCLogout clears local session then redirects to Authelia logout.
func (s *Server) OIDCLogout(c *gin.Context) {
	if token, err := c.Cookie("session"); err == nil {
		authMu.Lock()
		delete(sessions, token)
		authMu.Unlock()
	}
	c.SetCookie("session", "", -1, "/", "", false, true)
	if issuer := strings.TrimSuffix(os.Getenv("OIDC_ISSUER_URL"), "/"); issuer != "" {
		base := strings.TrimSuffix(os.Getenv("OIDC_REDIRECT_URL"), "/api/auth/oidc/callback")
		if base == "" {
			base = "/"
		}
		c.Redirect(http.StatusFound, issuer+"/logout?post_logout_redirect_uri="+base)
		return
	}
	c.Redirect(http.StatusFound, "/login")
}
