package services

import (
	"context"
	"errors"

	"github.com/ghmeier/rankanything/internal/auth"
	"github.com/ghmeier/rankanything/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var ErrEmailAlreadyRegistered = &emailAlreadyRegisteredError{}

type emailAlreadyRegisteredError struct{}

func (*emailAlreadyRegisteredError) Error() string { return "email already registered" }

type UserService struct {
	Queries  *db.Queries
	Sessions *auth.Sessions
}

type RegisterRequest struct {
	Email    string // already normalised
	Password string // already hashed
	Next     string
}

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

	return &user, nil
}

type LoginRequest struct {
	Email    string
	Password string
	Next     string
}

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

	return &user, nil
}

func (s *UserService) Logout(ctx context.Context) error {
	return s.Sessions.LogOut(ctx)
}

var ErrInvalidThemePreference = &invalidThemePreferenceError{}

type invalidThemePreferenceError struct{}

func (*invalidThemePreferenceError) Error() string { return "invalid theme preference" }

type UpdateThemePreferenceRequest struct {
	UserID     int64
	Preference db.UserThemePreference
}

func (s *UserService) UpdateThemePreference(ctx context.Context, req UpdateThemePreferenceRequest) (*db.User, error) {
	switch req.Preference {
	case db.UserThemePreferenceSystem, db.UserThemePreferenceLight, db.UserThemePreferenceDark:
	default:
		return nil, ErrInvalidThemePreference
	}

	user, err := s.Queries.UpdateUserThemePreference(ctx, db.UpdateUserThemePreferenceParams{
		ID:              req.UserID,
		ThemePreference: req.Preference,
	})
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
