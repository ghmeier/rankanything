// Package services holds the application's business logic — operations that
// manipulate domain state without knowledge of HTTP, templates, or the web.
// Each service receives structured input and returns structured output (or
// errors). The handlers layer is the only place that touches http.Request,
// http.ResponseWriter, or templates.
package services

import (
	"context"
	"errors"

	"github.com/ghmeier/rankanything/internal/auth"
	"github.com/ghmeier/rankanything/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrEmailAlreadyRegistered is returned when a registration attempt fails
// because the supplied email is already associated with an existing user.
var ErrEmailAlreadyRegistered = &emailAlreadyRegisteredError{}

type emailAlreadyRegisteredError struct{}

func (*emailAlreadyRegisteredError) Error() string { return "email already registered" }

// UserService implements user-facing business logic: registration, login,
// logout, and draft claiming.
type UserService struct {
	Queries  *db.Queries
	Sessions *auth.Sessions
}

// RegisterRequest is the input for user registration.
type RegisterRequest struct {
	Email    string // already normalised
	Password string // already hashed
	Next     string
}

// Register creates a user, logs them in, and claims any unclaimed drafts
// from this session. The handler is responsible for setting flash messages
// and performing the redirect.
func (s *UserService) Register(ctx context.Context, req RegisterRequest) (*db.User, error) {
	user, err := s.Queries.CreateUser(ctx, db.CreateUserParams{
		Email:        req.Email,
		PasswordHash: req.Password,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrEmailAlreadyRegistered
		}
		return nil, err
	}

	if err := s.Sessions.LogIn(ctx, user.ID); err != nil {
		return nil, err
	}

	_ = s.Queries.TouchLastLogin(ctx, user.ID)

	if err := s.claimDrafts(ctx, user.ID); err != nil {
		return nil, err
	}

	return &user, nil
}

// LoginRequest is the input for user login.
type LoginRequest struct {
	Email    string
	Password string
	Next     string
}

// Login authenticates a user, logs them in, touches their last login time,
// and claims any unclaimed drafts from this session. The handler is
// responsible for setting flash messages and performing the redirect.
func (s *UserService) Login(ctx context.Context, req LoginRequest) (*db.User, error) {
	user, err := s.Queries.GetUserByEmail(ctx, req.Email)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, auth.ErrInvalidCredentials
		}
		return nil, err
	}

	if !auth.CheckPassword(user.PasswordHash, req.Password) {
		return nil, auth.ErrInvalidCredentials
	}

	if err := s.Sessions.LogIn(ctx, user.ID); err != nil {
		return nil, err
	}

	_ = s.Queries.TouchLastLogin(ctx, user.ID)

	if err := s.claimDrafts(ctx, user.ID); err != nil {
		return nil, err
	}

	return &user, nil
}

// Logout destroys the current session.
func (s *UserService) Logout(ctx context.Context) error {
	return s.Sessions.LogOut(ctx)
}

// claimDrafts claims any unclaimed rankings belonging to this session and
// records them under the given user.
func (s *UserService) claimDrafts(ctx context.Context, userID int64) error {
	for _, slug := range s.Sessions.Drafts(ctx) {
		if err := s.claimOne(ctx, slug, userID); err != nil {
			return err
		}
	}
	return nil
}

func (s *UserService) claimOne(ctx context.Context, slug uuid.UUID, userID int64) error {
	_, err := s.Queries.ClaimRanking(ctx, db.ClaimRankingParams{
		Slug:   slug,
		UserID: &userID,
	})
	if err == nil {
		s.Sessions.ForgetDraft(ctx, slug)
	}
	return nil
}

// isUniqueViolation reports whether an error is a PostgreSQL unique constraint violation.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
