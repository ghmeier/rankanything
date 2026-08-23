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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// ErrTokenInvalid covers every reason a submitted verification or
// password-reset token can't be redeemed: unknown, expired, or already
// used. Callers can't distinguish which — same as token.Verify's own
// contract — so a caller can't fish for information by trying a token twice.
var ErrTokenInvalid = errors.New("This link has expired or was already used.")

// updateUserPasswordHash has no sqlc query behind it: db/queries and
// internal/db belong to another branch for the duration of this wave, and
// the users table already has every column this needs. Once that branch
// merges, this should become a generated query like the rest of the
// package's writes.
const updateUserPasswordHash = `UPDATE users SET password_hash = $1, updated_at = now() WHERE id = $2`

// passwordUpdater is the minimal capability VerificationService needs to run
// updateUserPasswordHash directly. *pgxpool.Pool and pgx.Tx both satisfy it,
// so tests can pass the same transaction they pass as Queries.
type passwordUpdater interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// VerificationService implements email verification and password reset:
// minting tokens, mailing them, and redeeming them.
type VerificationService struct {
	Queries *db.Queries
	Sender  email.Sender
	DB      passwordUpdater

	// Sessions is needed only to invalidate an account's sessions when its
	// password is reset.
	Sessions *auth.Sessions

	// BaseURL is the site's own absolute origin (e.g. "https://rankanything.app"),
	// used to build the links mailed to users. It comes from config.Config.BaseURL.
	BaseURL string
}

// SendVerificationEmail mints a fresh verification token for userID, stores
// its hash, and mails the plaintext link to to. It's called both right after
// registration and from the rankings index's resend control.
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

// RedeemEmailVerification validates plaintextToken against the stored hash
// and, if it's live and unused, marks both the verification row and the
// owning user as verified.
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

	// RedeemEmailVerification's WHERE clause re-checks is_verified/expires_at
	// atomically, so a concurrent redemption of the same token loses this
	// race rather than double-applying.
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

// RequestPasswordReset mints and mails a reset token when to belongs to a
// registered user. When it doesn't, this returns nil and sends nothing —
// the caller must respond identically either way, so a response can never
// be used to test which addresses have accounts.
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

// RedeemPasswordReset validates plaintextToken and, if it is live and
// unused, installs newPassword as the account's password and drops every
// session the account had.
//
// newPassword arrives in plaintext and is hashed here rather than by the
// caller, so that hashing happens strictly after the token check. Hashing
// first would let an unauthenticated caller burn a full bcrypt round per
// request using a token they invented, and nothing in the app rate-limits
// this endpoint yet.
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

	if _, err := s.DB.Exec(ctx, updateUserPasswordHash, passwordHash, row.UserID); err != nil {
		return fmt.Errorf("password reset: update password: %w", err)
	}

	// A reset is how someone takes an account back, so every session opened
	// against the old password has to stop working. The caller's own session
	// is not covered here; see auth.Sessions.DestroyAllForUser.
	if err := s.Sessions.DestroyAllForUser(ctx, row.UserID); err != nil {
		return fmt.Errorf("password reset: drop sessions: %w", err)
	}
	return nil
}
