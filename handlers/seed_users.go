package handlers

import (
	"context"
	"log/slog"
	"os"
	"time"

	"golang.org/x/crypto/bcrypt"
	"ledit/ent"
	"ledit/ent/user"
)

func seedUsers(client *ent.Client, ctx context.Context) {
	if os.Getenv("LEDIT_AUTH_DISABLE") == "true" || os.Getenv("LEDIT_AUTH_DISABLE") == "1" {
		return
	}
	count, err := client.User.Query().Count(ctx)
	if err != nil {
		slog.Error("Failed to count users for seeding", "error", err)
		return
	}
	if count > 0 {
		return
	}
	// Try legacy admin hash.
	if admin, err := client.AdminSettings.Query().First(ctx); err == nil && admin.PasswordHash != "" {
		username := admin.Username
		if username == "" {
			username = "admin"
		}
		if _, err := client.User.Create().SetUsername(username).SetPasswordHash(admin.PasswordHash).SetRole(user.RoleAdmin).SetCreatedAt(time.Now()).Save(ctx); err == nil {
			slog.Info("Seeded admin user from legacy AdminSettings", "username", username)
			return
		}
	}
	// Try env password.
	if pwd := os.Getenv("LEDIT_ADMIN_PASSWORD"); pwd != "" {
		hash, _ := bcrypt.GenerateFromPassword([]byte(pwd), bcrypt.DefaultCost)
		if _, err := client.User.Create().SetUsername("admin").SetPasswordHash(string(hash)).SetRole(user.RoleAdmin).SetCreatedAt(time.Now()).Save(ctx); err == nil {
			slog.Info("Seeded admin user from env", "username", "admin")
			return
		}
	}
	// Otherwise leave empty; setup wizard will create.
	slog.Info("No users found; setup wizard will create first admin")
}
