package handlers

import (
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"ledit/ent"
	"ledit/ent/apitoken"
	"ledit/internal/account"
)

// legalData is the operator identity plus the deployment facts a published
// policy is allowed to assert. Values come from the environment or the live
// settings, so the text cannot claim a processor the instance does not run.
type legalData struct {
	Controller     string
	ContactEmail   string
	ContactAddress string
	HostingRegion  string
	BaseURL        string
	AppName        string
	Year           int
	SignedIn       bool
	Username       string
	Analytics      bool
	AnalyticsURL   string
	Traces         bool
	Mail           bool
	OIDC           bool
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func (s *Server) legalInfo(c *gin.Context) legalData {
	d := legalData{
		Controller:    "the operator of this LEDit instance",
		HostingRegion: "the European Union",
		BaseURL:       strings.TrimRight(envOr("BASE_URL", "http://localhost:8080"), "/"),
		AppName:       "LEDit",
		Year:          time.Now().Year(),
	}
	d.ContactEmail = strings.TrimSpace(os.Getenv("LEGAL_CONTACT_EMAIL"))
	d.ContactAddress = strings.TrimSpace(os.Getenv("LEGAL_CONTACT_ADDRESS"))
	if v := strings.TrimSpace(os.Getenv("LEGAL_CONTROLLER_NAME")); v != "" {
		d.Controller = v
	}
	if v := strings.TrimSpace(os.Getenv("LEGAL_HOSTING_REGION")); v != "" {
		d.HostingRegion = v
	}
	if s.Telemetry != nil && s.Telemetry.IsEnabled() {
		d.Traces = true
	}
	if s.DB != nil {
		if um, err := s.DB.UmamiSettings.Query().First(c.Request.Context()); err == nil && um.Enable {
			d.Analytics = true
			d.AnalyticsURL = um.Endpoint
		}
		if gs, err := s.DB.GeneralSettings.Query().WithEmailSettings().First(c.Request.Context()); err == nil {
			if es, err := gs.Edges.EmailSettingsOrErr(); err == nil {
				for _, e := range es {
					if e != nil && strings.TrimSpace(e.Host) != "" {
						d.Mail = true
					}
				}
			}
		}
		d.OIDC = oidcEnabled()
	}
	if u, ok := s.accountUser(c); ok {
		d.SignedIn = true
		d.Username = u.Username
	}
	return d
}

func (s *Server) privacyPage(c *gin.Context) {
	d := s.legalInfo(c)
	s.renderPage(c, http.StatusOK, "privacy.html", gin.H{
		"title":    "Privacy",
		"Legal":    d,
		"active":   "privacy",
		"BaseURL":  d.BaseURL,
		"SignedIn": d.SignedIn,
		"Username": d.Username,
	})
}

func (s *Server) termsPage(c *gin.Context) {
	d := s.legalInfo(c)
	s.renderPage(c, http.StatusOK, "terms.html", gin.H{
		"title":    "Terms",
		"Legal":    d,
		"active":   "terms",
		"BaseURL":  d.BaseURL,
		"SignedIn": d.SignedIn,
		"Username": d.Username,
	})
}

func (s *Server) accountDeletePage(c *gin.Context) {
	d := s.legalInfo(c)
	s.renderPage(c, http.StatusOK, "account_delete.html", gin.H{
		"title":    "Delete your account",
		"Legal":    d,
		"active":   "account-delete",
		"SignedIn": d.SignedIn,
		"Username": d.Username,
		"deleted":  c.Query("deleted") == "1",
		"error":    c.Query("error"),
	})
}

// accountUser resolves the session or bearer credential to a real user row.
// Bearer tokens carry an owner id from the API-token administration table; if
// that id does not match a user row, there is no account to act on.
func (s *Server) accountUser(c *gin.Context) (*ent.User, bool) {
	if !authEnabled {
		return nil, false
	}
	if token, err := c.Cookie("session"); err == nil {
		if sd, ok := lookupSession(s.DB, token); ok {
			if u, err := s.DB.User.Get(c.Request.Context(), sd.UserID); err == nil {
				return u, true
			}
		}
	}
	auth := c.GetHeader("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return nil, false
	}
	secret := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	if secret == "" {
		return nil, false
	}
	tok, err := s.DB.ApiToken.Query().
		Where(apitoken.TokenHashEQ(hashAPIToken(secret))).
		Only(c.Request.Context())
	if err != nil || tok.RevokedAt != nil || (tok.ExpiresAt != nil && !tok.ExpiresAt.After(time.Now())) {
		return nil, false
	}
	if tok.OwnerID <= 0 {
		return nil, false
	}
	u, err := s.DB.User.Get(c.Request.Context(), tok.OwnerID)
	if err != nil {
		return nil, false
	}
	return u, true
}

const ctxAccountUser = "account_user"

// RequireAccount accepts a browser session or an API bearer token and puts the
// resolved user in the request context.
func (s *Server) RequireAccount() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !authEnabled {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"error": "authentication is disabled on this instance",
			})
			return
		}
		u, ok := s.accountUser(c)
		if !ok {
			c.Header("WWW-Authenticate", `Bearer realm="LEDit"`)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Set(ctxAccountUser, u)
		c.Next()
	}
}

func accountFromContext(c *gin.Context) *ent.User {
	if v, ok := c.Get(ctxAccountUser); ok {
		if u, ok := v.(*ent.User); ok {
			return u
		}
	}
	return nil
}

// requireSessionAccount is the stricter variant used by the browser settings
// route: a bearer token cannot silently delete the account it owns.
func (s *Server) requireSessionAccount() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !authEnabled {
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}
		token, err := c.Cookie("session")
		if err != nil {
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}
		sd, ok := lookupSession(s.DB, token)
		if !ok {
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}
		u, err := s.DB.User.Get(c.Request.Context(), sd.UserID)
		if err != nil {
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}
		c.Set(ctxAccountUser, u)
		c.Next()
	}
}

func deletionErrorStatus(err error) int {
	switch {
	case errors.Is(err, account.ErrLastAdmin):
		return http.StatusConflict
	case errors.Is(err, account.ErrConfirmation), errors.Is(err, account.ErrInvalid):
		return http.StatusBadRequest
	case errors.Is(err, account.ErrNotFound):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}

// SettingsAccountDelete is the browser form target. It requires a session,
// checks the typed username, then deletes through the shared service.
func (s *Server) SettingsAccountDelete(c *gin.Context) {
	u := accountFromContext(c)
	if u == nil {
		c.Redirect(http.StatusFound, "/login")
		return
	}
	confirm := c.PostForm("confirm")
	if strings.TrimSpace(confirm) == "" {
		confirm = c.Query("confirm")
	}
	_, err := account.New(s.DB).Delete(c.Request.Context(), u.ID, confirm)
	if err != nil {
		code := "failed"
		if errors.Is(err, account.ErrLastAdmin) {
			code = "last_admin"
		} else if errors.Is(err, account.ErrConfirmation) {
			code = "confirm"
		}
		c.Redirect(http.StatusSeeOther, "/account/delete?error="+code)
		return
	}
	clearUserSessions(s.DB, u.ID)
	clearSessionCookie(c)
	c.Redirect(http.StatusSeeOther, "/account/delete?deleted=1")
}

// APIMeDelete is the JSON deletion endpoint. The confirm field must equal the
// current username (an optional leading @ is accepted).
func (s *Server) APIMeDelete(c *gin.Context) {
	u := accountFromContext(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var body struct {
		Confirm string `json:"confirm"`
	}
	_ = c.ShouldBindJSON(&body)
	if strings.TrimSpace(body.Confirm) == "" {
		body.Confirm = c.Query("confirm")
	}
	_, err := account.New(s.DB).Delete(c.Request.Context(), u.ID, body.Confirm)
	if err != nil {
		status := deletionErrorStatus(err)
		payload := gin.H{"error": err.Error()}
		if errors.Is(err, account.ErrLastAdmin) {
			payload["code"] = "last_admin"
			payload["explain"] = "This is the only administrator. Promote another admin first, then delete this account."
		} else if errors.Is(err, account.ErrConfirmation) {
			payload["code"] = "confirmation_required"
		}
		c.JSON(status, payload)
		return
	}
	clearUserSessions(s.DB, u.ID)
	clearSessionCookie(c)
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

// accountExport is the user's own data. Secrets are never included: tokens
// appear as metadata only, exactly as they do in the token listing.
type accountExport struct {
	Profile profileExport      `json:"profile"`
	Tokens  []accountTokenView `json:"api_tokens"`
}

type profileExport struct {
	ID          int        `json:"id"`
	Username    string     `json:"username"`
	Role        string     `json:"role"`
	Email       *string    `json:"email,omitempty"`
	AuthMethod  string     `json:"auth_method"`
	CreatedAt   time.Time  `json:"created_at"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
}

type accountTokenView struct {
	ID         int        `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Role       string     `json:"role"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

// APIMeExport returns the account profile and token metadata as JSON.
func (s *Server) APIMeExport(c *gin.Context) {
	u := accountFromContext(c)
	if u == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	out := accountExport{
		Profile: profileExport{
			ID:          u.ID,
			Username:    u.Username,
			Role:        string(u.Role),
			Email:       u.Email,
			AuthMethod:  string(u.AuthMethod),
			CreatedAt:   u.CreatedAt,
			LastLoginAt: u.LastLoginAt,
		},
		Tokens: []accountTokenView{},
	}
	tokens, err := s.DB.ApiToken.Query().
		Where(apitoken.OwnerIDEQ(u.ID)).
		Order(ent.Desc(apitoken.FieldCreatedAt)).
		All(c.Request.Context())
	if err == nil {
		for _, t := range tokens {
			out.Tokens = append(out.Tokens, accountTokenView{
				ID:         t.ID,
				Name:       t.Name,
				Prefix:     t.TokenPrefix,
				Role:       string(t.Role),
				CreatedAt:  t.CreatedAt,
				ExpiresAt:  t.ExpiresAt,
				RevokedAt:  t.RevokedAt,
				LastUsedAt: t.LastUsedAt,
			})
		}
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, out)
}
