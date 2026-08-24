package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"ledit/ent/adminsettings"
)

const resetTokenTTL = 30 * time.Minute

var (
	resetMu     sync.Mutex
	resetTokens = map[string]time.Time{} // sha256(token) -> expiry
)

// generateResetToken returns a new unguessable reset token (raw) for emailing.
func generateResetToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		slog.Error("failed to read random bytes for reset token", "error", err)
		return ""
	}
	return hex.EncodeToString(b)
}

// storeResetToken records a token hash with its expiry.
func storeResetToken(raw string) {
	resetMu.Lock()
	defer resetMu.Unlock()
	// Replace any prior token by wiping the store; this is a single-admin
	// app, so a fresh request supersedes old links.
	resetTokens = map[string]time.Time{hashSessionToken(raw): time.Now().Add(resetTokenTTL)}
}

// consumeResetToken validates a raw token, deletes it (single use), and
// reports whether it was valid and unexpired.
func consumeResetToken(raw string) bool {
	if raw == "" {
		return false
	}
	hash := hashSessionToken(raw)
	resetMu.Lock()
	defer resetMu.Unlock()
	expiry, ok := resetTokens[hash]
	if !ok {
		return false
	}
	delete(resetTokens, hash)
	return time.Now().Before(expiry)
}

// validResetToken reports whether a raw token is present and unexpired
// without consuming it (used when rendering the reset form).
func validResetToken(raw string) bool {
	if raw == "" {
		return false
	}
	resetMu.Lock()
	defer resetMu.Unlock()
	expiry, ok := resetTokens[hashSessionToken(raw)]
	return ok && time.Now().Before(expiry)
}

// ForgotPasswordPage renders the "request a reset link" form.
func (s *Server) ForgotPasswordPage(c *gin.Context) {
	c.HTML(http.StatusOK, "forgot_password.html", gin.H{})
}

// ForgotPasswordAction mails a reset link to the alert recipient if the
// submitted username matches the admin account. It always reports success to
// avoid revealing whether the account exists.
func (s *Server) ForgotPasswordAction(c *gin.Context) {
	username := c.PostForm("username")

	admin, err := s.DB.AdminSettings.Query().Where(adminsettings.Username(username)).First(s.Ctx)
	successMsg := "If an account exists for that username, a password reset link has been emailed."
	if err != nil {
		// No such user: answer identically to a successful request.
		c.HTML(http.StatusOK, "forgot_password.html", gin.H{"info": successMsg})
		return
	}

	if err := s.mailPasswordReset(c, admin.ID); err != nil {
		slog.Error("failed to send password reset email", "error", err)
		c.HTML(http.StatusOK, "forgot_password.html", gin.H{"error": "Could not send a reset link. Check the email settings and that a recipient email is configured."})
		return
	}

	c.HTML(http.StatusOK, "forgot_password.html", gin.H{"info": successMsg})
}

// mailPasswordReset builds a reset link and sends it via SMTP to the alert
// recipient email address.
func (s *Server) mailPasswordReset(c *gin.Context, _ int) error {
	emailSettings, err := s.DB.EmailSettings.Query().First(s.Ctx)
	if err != nil {
		return fmt.Errorf("email settings not configured: %w", err)
	}
	alertSettings, err := s.DB.AlertSettings.Query().First(s.Ctx)
	if err != nil {
		return fmt.Errorf("alert settings not configured: %w", err)
	}
	recipient := alertSettings.RecipientEmail
	if recipient == "" {
		return fmt.Errorf("no recipient email configured")
	}

	sender := &EmailSender{
		Host:     emailSettings.Host,
		Port:     emailSettings.Port,
		Username: emailSettings.Username,
		Password: emailSettings.Password,
		From:     emailSettings.FromAddress,
		UseTLS:   emailSettings.UseTLS,
	}
	if !sender.Enabled() {
		return fmt.Errorf("email settings incomplete")
	}

	raw := generateResetToken()
	if raw == "" {
		return fmt.Errorf("failed to generate reset token")
	}
	storeResetToken(raw)

	scheme := "http"
	if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	resetURL := fmt.Sprintf("%s://%s/reset-password?token=%s", scheme, c.Request.Host, raw)

	subject := "LEDit password reset"
	body := fmt.Sprintf("A password reset was requested for your LEDit account.\n\nOpen the link below to choose a new password (valid for %d minutes):\n\n%s\n\nIf you did not request this, you can ignore this email.", int(resetTokenTTL.Minutes()), resetURL)
	return sender.SendMessage(recipient, subject, body)
}

// ResetPasswordPage renders the new-password form when the token is valid.
func (s *Server) ResetPasswordPage(c *gin.Context) {
	token := c.Query("token")
	if !validResetToken(token) {
		c.HTML(http.StatusBadRequest, "reset_password.html", gin.H{"error": "This password reset link is invalid or has expired."})
		return
	}
	c.HTML(http.StatusOK, "reset_password.html", gin.H{"token": token})
}

// ResetPasswordAction applies a new password for the admin account when the
// token is valid, then logs out any existing sessions.
func (s *Server) ResetPasswordAction(c *gin.Context) {
	token := c.PostForm("token")
	newPass := c.PostForm("new_password")
	confirm := c.PostForm("confirm_password")

	// Validate the token without consuming it so a failed attempt (bad input,
	// hashing error, DB error) can be retried.
	if !validResetToken(token) {
		c.HTML(http.StatusBadRequest, "reset_password.html", gin.H{"error": "This password reset link is invalid or has expired."})
		return
	}
	if newPass == "" {
		c.HTML(http.StatusOK, "reset_password.html", gin.H{"token": token, "error": "New password cannot be empty"})
		return
	}
	if newPass != confirm {
		c.HTML(http.StatusOK, "reset_password.html", gin.H{"token": token, "error": "New passwords do not match"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPass), bcrypt.DefaultCost)
	if err != nil {
		c.HTML(http.StatusOK, "reset_password.html", gin.H{"token": token, "error": "Failed to hash password"})
		return
	}

	// There is exactly one admin account in this app.
	if _, err := s.DB.AdminSettings.Update().SetPasswordHash(string(hash)).Save(s.Ctx); err != nil {
		slog.Error("failed to save new admin password", "error", err)
		c.HTML(http.StatusOK, "reset_password.html", gin.H{"token": token, "error": "Failed to save new password"})
		return
	}
	// Also update user table if exists.
	if users, err := s.DB.User.Query().All(s.Ctx); err == nil {
		for _, u := range users {
			if u.Role == "admin" {
				_, _ = s.DB.User.UpdateOneID(u.ID).SetPasswordHash(string(hash)).Save(s.Ctx)
				break
			}
		}
	}

	// Password updated: the token is now single-use and all existing sessions
	// are invalidated so the old password is fully dead.
	consumeResetToken(token)
	authMu.Lock()
	sessions = map[string]sessionData{}
	authMu.Unlock()

	c.Redirect(http.StatusFound, "/login?reset=1")
}
