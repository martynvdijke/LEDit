// Package account owns operations that act on a user's whole account. The web
// handler and the JSON API share this service so their behaviour cannot drift.
package account

import (
	"context"
	"errors"
	"strings"
	"time"

	"ledit/ent"
	"ledit/ent/apitoken"
	"ledit/ent/user"
	"ledit/ent/websession"
)

var (
	// ErrLastAdmin means closing the account would leave the instance with no
	// administrator able to sign in.
	ErrLastAdmin = errors.New("the last active admin cannot be deleted")
	// ErrNotFound means there is no account with the requested ID.
	ErrNotFound = errors.New("account not found")
	// ErrInvalid means the request was malformed before it reached the store.
	ErrInvalid = errors.New("invalid account request")
	// ErrConfirmation means the typed confirmation did not match the username.
	ErrConfirmation = errors.New("the confirmation does not match the username")
)

// Service performs whole-account operations.
type Service struct {
	Client *ent.Client
}

// New builds the shared account service.
func New(client *ent.Client) *Service { return &Service{Client: client} }

// Delete removes the account and every credential that could still act as it.
// The last admin cannot be deleted. Everything happens in one transaction, so
// a failure leaves the account untouched.
//
// API tokens are revoked when their owner_id matches the account. LEDit's API
// tokens are instance credentials created by an administrator; there is no
// per-user owner column to match beyond that. Guest-remote tokens are likewise
// instance-scoped (the guesttoken schema has no owner) and are not personal
// account data, so they are not revoked by one user's closure.
func (s *Service) Delete(ctx context.Context, userID int, confirm string) (*ent.User, error) {
	if s.Client == nil || userID <= 0 {
		return nil, ErrInvalid
	}
	tx, err := s.Client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	row, err := tx.User.Get(ctx, userID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if !strings.EqualFold(strings.TrimSpace(confirm), row.Username) &&
		!strings.EqualFold(strings.TrimSpace(confirm), "@"+row.Username) {
		return nil, ErrConfirmation
	}
	if row.Role == user.RoleAdmin {
		admins, err := tx.User.Query().Where(user.RoleEQ(user.RoleAdmin)).Count(ctx)
		if err != nil {
			return nil, err
		}
		if admins <= 1 {
			return nil, ErrLastAdmin
		}
	}

	now := time.Now()
	if _, err := tx.WebSession.Delete().Where(websession.UserID(userID)).Exec(ctx); err != nil {
		return nil, err
	}
	if _, err := tx.ApiToken.Update().
		Where(apitoken.OwnerIDEQ(userID), apitoken.RevokedAtIsNil()).
		SetRevokedAt(now).
		Save(ctx); err != nil {
		return nil, err
	}
	// Deleting the row clears the password hash, email, OIDC subject and login
	// method in one step. Devices, playlists, frames and other instance content
	// are independent of the user row and are deliberately preserved.
	if err := tx.User.DeleteOneID(userID).Exec(ctx); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return row, nil
}
