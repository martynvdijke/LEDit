package handlers

import (
	"testing"
	"time"

	"ledit/ent/user"
)

// Covers the "User auto-provision/link by email + groups->admin" requirement:
// linking by verified email, first-login provisioning, and role sync from the
// `groups` claim on every login.

func TestLinkOIDCUserLinksExistingEmail(t *testing.T) {
	srv := oidcTestServer(t, true)

	existing := srv.DB.User.Create().
		SetUsername("martyn").
		SetPasswordHash("$2a$10$existinghash").
		SetRole(user.RoleAdmin).
		SetEmail("martyn@example.test").
		SetCreatedAt(time.Now()).
		SaveX(srv.Ctx)

	linked, err := srv.linkOIDCUser("sub-42", "martyn@example.test", "admin", "martyn", "Martyn")
	if err != nil {
		t.Fatalf("linkOIDCUser: %v", err)
	}
	if linked.ID != existing.ID {
		t.Fatalf("expected the existing user to be linked, got a new user %d", linked.ID)
	}
	if linked.OidcSub == nil || *linked.OidcSub != "sub-42" {
		t.Fatalf("oidc_sub not linked: %+v", linked.OidcSub)
	}
	if linked.AuthMethod != user.AuthMethodOidc {
		t.Fatalf("auth_method should become oidc, got %q", linked.AuthMethod)
	}
	// Linking must not change the username or the password hash, so password
	// login keeps working for the same account.
	if linked.Username != "martyn" || linked.PasswordHash != existing.PasswordHash {
		t.Fatalf("linking modified identity fields: %+v", linked)
	}
}

func TestLinkOIDCUserReturnsBySubject(t *testing.T) {
	srv := oidcTestServer(t, true)

	first, err := srv.linkOIDCUser("sub-7", "user@example.test", "admin", "user", "User")
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	// Groups claim no longer contains `admins` → role must be downgraded.
	again, err := srv.linkOIDCUser("sub-7", "user@example.test", "viewer", "user", "User")
	if err != nil {
		t.Fatalf("relink: %v", err)
	}
	if again.ID != first.ID {
		t.Fatalf("expected the same user on re-login, got %d then %d", first.ID, again.ID)
	}
	if again.Role != user.RoleViewer {
		t.Fatalf("role should follow the groups claim, got %q", again.Role)
	}
	// And back up again.
	admin, err := srv.linkOIDCUser("sub-7", "user@example.test", "admin", "user", "User")
	if err != nil {
		t.Fatalf("re-admin: %v", err)
	}
	if admin.Role != user.RoleAdmin {
		t.Fatalf("role should be restored to admin, got %q", admin.Role)
	}
	if admin.LastLoginAt == nil {
		t.Error("last_login_at should be stamped on OIDC login")
	}
}

func TestLinkOIDCUserProvisionsFirstLogin(t *testing.T) {
	srv := oidcTestServer(t, true)

	provisioned, err := srv.linkOIDCUser("sub-9", "new.person@example.test", "viewer", "", "")
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if provisioned.Email == nil || *provisioned.Email != "new.person@example.test" {
		t.Fatalf("email not stored: %+v", provisioned.Email)
	}
	if provisioned.OidcSub == nil || *provisioned.OidcSub != "sub-9" {
		t.Fatalf("oidc_sub not stored: %+v", provisioned.OidcSub)
	}
	if provisioned.Role != user.RoleViewer {
		t.Fatalf("provisioned role should follow groups, got %q", provisioned.Role)
	}
	if provisioned.AuthMethod != user.AuthMethodOidc {
		t.Fatalf("provisioned auth_method should be oidc, got %q", provisioned.AuthMethod)
	}
	// The generated username must satisfy the schema and be usable in admin UI.
	if len(provisioned.Username) < 3 {
		t.Fatalf("generated username too short: %q", provisioned.Username)
	}
	// An unusable random password keeps OIDC-only accounts out of password login.
	if provisioned.PasswordHash == "" {
		t.Error("provisioned user needs a (random) password hash")
	}
}

func TestLinkOIDCUserProvisionsAdminFromGroups(t *testing.T) {
	srv := oidcTestServer(t, true)

	provisioned, err := srv.linkOIDCUser("sub-10", "boss@example.test", "admin", "boss", "Boss")
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if provisioned.Role != user.RoleAdmin {
		t.Fatalf("`admins` group should grant admin, got %q", provisioned.Role)
	}

	// Two different subjects with the same email must not duplicate accounts:
	// the email lookup wins and the subject is replaced.
	relinked, err := srv.linkOIDCUser("sub-11", "boss@example.test", "admin", "boss", "Boss")
	if err != nil {
		t.Fatalf("relink: %v", err)
	}
	if relinked.ID != provisioned.ID {
		t.Fatalf("same email must resolve to the same user, got %d and %d", provisioned.ID, relinked.ID)
	}
	if *relinked.OidcSub != "sub-11" {
		t.Fatalf("oidc_sub should follow the latest verified subject, got %q", *relinked.OidcSub)
	}
	count, err := srv.DB.User.Query().Where(user.EmailEQ("boss@example.test")).Count(srv.Ctx)
	if err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected a single user for that email, found %d", count)
	}
}
