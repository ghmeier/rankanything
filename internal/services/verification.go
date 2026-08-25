package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ghmeier/rankanything/internal/auth"
	"github.com/ghmeier/rankanything/internal/db"
	"github.com/ghmeier/rankanything/internal/email"
	"github.com/ghmeier/rankanything/internal/token"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var ErrTokenInvalid = errors.New("This link has expired or was already used.")

type VerificationService struct {
	Queries *db.Queries
	Sender  email.Sender

	Sessions *auth.Sessions
	BaseURL  string
}

func (s *VerificationService) SendVerificationEmail(ctx context.Context, userID int64, to string) error {
	plaintext, hash, expiresAt, err := token.Generate(token.VerificationTTL)
	if err != nil {
		return fmt.Errorf("verification: generate token: %w", err)
	}

	if _, err := s.Queries.CreateEmailVerification(ctx, db.CreateEmailVerificationParams{
		TokenHash: hash,
		ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true},
		UserID:    userID,
	}); err != nil {
		return fmt.Errorf("verification: store token: %w", err)
	}

	msg, err := email.VerificationMessage(to, s.BaseURL, plaintext)
	if err != nil {
		return fmt.Errorf("verification: render message: %w", err)
	}
	return s.Sender.Send(ctx, msg)
}

func (s *VerificationService) RedeemEmailVerification(ctx context.Context, plaintextToken string) error {
	hash := token.Hash(plaintextToken)

	row, err := s.Queries.GetEmailVerificationByTokenHash(ctx, hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTokenInvalid
		}
		return fmt.Errorf("verification: look up token: %w", err)
	}

	if !token.Verify(plaintextToken, row.TokenHash, row.ExpiresAt.Time, time.Now(), row.IsVerified) {
		return ErrTokenInvalid
	}

	if _, err := s.Queries.RedeemEmailVerification(ctx, hash); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTokenInvalid
		}
		return fmt.Errorf("verification: redeem token: %w", err)
	}

	if _, err := s.Queries.MarkUserEmailVerified(ctx, row.UserID); err != nil {
		return fmt.Errorf("verification: mark user verified: %w", err)
	}
	return nil
}

// Returns nil for unknown addresses to prevent account enumeration.
func (s *VerificationService) RequestPasswordReset(ctx context.Context, to string) error {
	user, err := s.Queries.GetUserByEmail(ctx, to)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("password reset: look up user: %w", err)
	}

	plaintext, hash, expiresAt, err := token.Generate(token.ResetTTL)
	if err != nil {
		return fmt.Errorf("password reset: generate token: %w", err)
	}

	if _, err := s.Queries.CreatePasswordReset(ctx, db.CreatePasswordResetParams{
		TokenHash: hash,
		ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true},
		UserID:    user.ID,
	}); err != nil {
		return fmt.Errorf("password reset: store token: %w", err)
	}

	msg, err := email.PasswordResetMessage(user.Email, s.BaseURL, plaintext)
	if err != nil {
		return fmt.Errorf("password reset: render message: %w", err)
	}
	return s.Sender.Send(ctx, msg)
}

// Hashes after the token check so callers cannot burn bcrypt rounds with bad tokens.
func (s *VerificationService) RedeemPasswordReset(ctx context.Context, plaintextToken, newPassword string) error {
	hash := token.Hash(plaintextToken)

	row, err := s.Queries.GetPasswordResetByTokenHash(ctx, hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTokenInvalid
		}
		return fmt.Errorf("password reset: look up token: %w", err)
	}

	if !token.Verify(plaintextToken, row.TokenHash, row.ExpiresAt.Time, time.Now(), row.IsUsed) {
		return ErrTokenInvalid
	}

	passwordHash, err := auth.HashPassword(newPassword)
	if err != nil {
		return err
	}

	if _, err := s.Queries.RedeemPasswordReset(ctx, hash); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTokenInvalid
		}
		return fmt.Errorf("password reset: redeem token: %w", err)
	}

	if err := s.Queries.UpdateUserPasswordHash(ctx, db.UpdateUserPasswordHashParams{
		ID:           row.UserID,
		PasswordHash: passwordHash,
	}); err != nil {
		return fmt.Errorf("password reset: update password: %w", err)
	}

	if err := s.Sessions.DestroyAllForUser(ctx, row.UserID); err != nil {
		return fmt.Errorf("password reset: drop sessions: %w", err)
	}
	return nil
}
