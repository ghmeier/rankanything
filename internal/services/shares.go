package services

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ghmeier/rankanything/internal/db"
	"github.com/ghmeier/rankanything/internal/email"
	"github.com/ghmeier/rankanything/internal/token"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	ErrShareNotPublic       = errors.New("Share not found.")
	ErrShareNotFound        = errors.New("Share not found.")
	ErrInviteExpired        = errors.New("This invitation has expired.")
	ErrInviteAlreadyRedeemed = errors.New("This invitation has already been used.")
)

const InviteTTL = 7 * 24 * time.Hour

type ShareService struct {
	Queries     *db.Queries
	Pool        txBeginner
	EmailSender email.Sender
	BaseURL     string
}

type LinkShare struct {
	IsPublic bool
	URL      string
}

type ShareValidation struct {
	Shareable bool
	Reasons   []string
}

func (s *ShareService) ValidateShareable(ctx context.Context, ranking db.Ranking) (ShareValidation, error) {
	versions, err := s.Queries.ListRankingVersionsForRanking(ctx, ranking.ID)
	if err != nil {
		return ShareValidation{}, err
	}
	hasPublished := false
	for _, v := range versions {
		if v.PublishedAt.Valid {
			hasPublished = true
			break
		}
	}

	owner, err := s.Queries.GetUserByID(ctx, ranking.UserID)
	if err != nil {
		return ShareValidation{}, err
	}

	var reasons []string
	if !hasPublished {
		reasons = append(reasons, "Publish at least one version.")
	}
	if !owner.EmailVerified {
		reasons = append(reasons, "Verify your email.")
	}
	return ShareValidation{Shareable: len(reasons) == 0, Reasons: reasons}, nil
}

func (s *ShareService) GetLinkShare(ctx context.Context, rankingID int64) (LinkShare, error) {
	shares, err := s.Queries.ListRankingSharesForRanking(ctx, rankingID)
	if err != nil {
		return LinkShare{}, err
	}
	for _, share := range shares {
		// The link share is the row with neither a user nor an email.
		if share.UserID != nil || share.Email != nil {
			continue
		}
		if !share.IsPublic || share.PublicSlug == nil {
			return LinkShare{}, nil
		}
		return LinkShare{IsPublic: true, URL: s.PublicURL(*share.PublicSlug)}, nil
	}
	return LinkShare{}, nil
}

func (s *ShareService) EnableLinkShare(ctx context.Context, rankingID int64) (LinkShare, error) {
	const maxAttempts = 5
	var lastErr error
	for range maxAttempts {
		slug, err := newPublicSlug()
		if err != nil {
			return LinkShare{}, err
		}

		share, err := s.Queries.UpsertRankingLinkShare(ctx, db.UpsertRankingLinkShareParams{
			RankingID:  rankingID,
			PublicSlug: &slug,
		})
		if err == nil {
			return LinkShare{IsPublic: true, URL: s.PublicURL(*share.PublicSlug)}, nil
		}
		if !isUniqueViolation(err) {
			return LinkShare{}, err
		}
		lastErr = err
	}
	return LinkShare{}, fmt.Errorf("mint public slug: %w", lastErr)
}

func (s *ShareService) DisableLinkShare(ctx context.Context, rankingID int64) error {
	return s.Queries.ClearRankingPublicSlug(ctx, rankingID)
}

type PublicRanking struct {
	Ranking db.Ranking
	Version db.RankingVersion
}

func (s *ShareService) ResolvePublicRanking(ctx context.Context, slug string) (PublicRanking, error) {
	share, err := s.Queries.GetRankingShareByPublicSlug(ctx, &slug)
	if err != nil || !share.IsPublic {
		return PublicRanking{}, ErrShareNotPublic
	}

	ranking, err := s.Queries.GetRankingByID(ctx, share.RankingID)
	if err != nil {
		return PublicRanking{}, ErrShareNotPublic
	}

	// A draft-only ranking must not be exposed publicly.
	version, err := s.Queries.ResolveLiveRankingVersion(ctx, ranking.ID)
	if err != nil || !version.PublishedAt.Valid {
		return PublicRanking{}, ErrShareNotPublic
	}

	return PublicRanking{Ranking: ranking, Version: version}, nil
}

func (s *ShareService) PublicURL(slug string) string {
	return strings.TrimRight(s.BaseURL, "/") + "/s/" + slug
}

func newPublicSlug() (string, error) {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate public slug: %w", err)
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)), nil
}

type InviteRequest struct {
	RankingID     int64
	Email         string
	Role          db.RankingShareRole
	InviterUserID int64
	InviterName   string
	RankingName   string
}

func (s *ShareService) InviteByEmail(ctx context.Context, req InviteRequest) (db.RankingShare, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return db.RankingShare{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	txq := s.Queries.WithTx(tx)

	share, err := txq.CreateEmailShare(ctx, db.CreateEmailShareParams{
		RankingID: req.RankingID,
		Email:     &req.Email,
		Role:      req.Role,
	})
	if err != nil {
		return db.RankingShare{}, fmt.Errorf("create email share: %w", err)
	}

	plaintext, hash, expiresAt, err := token.Generate(InviteTTL)
	if err != nil {
		return db.RankingShare{}, err
	}

	_, err = txq.CreateRankingInvite(ctx, db.CreateRankingInviteParams{
		Token:          hash,
		UserID:         req.InviterUserID,
		InvitedEmail:   &req.Email,
		RankingShareID: share.ID,
		ExpiresAt:      pgtype.Timestamptz{Time: expiresAt, Valid: true},
	})
	if err != nil {
		return db.RankingShare{}, fmt.Errorf("create ranking invite: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return db.RankingShare{}, fmt.Errorf("commit invite: %w", err)
	}

	msg, err := email.InviteMessage(req.Email, req.InviterName, req.RankingName, string(req.Role), s.BaseURL, plaintext)
	if err != nil {
		return db.RankingShare{}, fmt.Errorf("build invite email: %w", err)
	}
	if err := s.EmailSender.Send(ctx, msg); err != nil {
		return db.RankingShare{}, fmt.Errorf("send invite email: %w", err)
	}

	return share, nil
}

func (s *ShareService) ListEmailShares(ctx context.Context, rankingID int64) ([]db.RankingShare, error) {
	return s.Queries.ListEmailSharesForRanking(ctx, rankingID)
}

func (s *ShareService) RemoveShare(ctx context.Context, shareID, rankingID int64) error {
	share, err := s.Queries.GetRankingShareByID(ctx, shareID)
	if err != nil {
		return ErrShareNotFound
	}
	if share.RankingID != rankingID {
		return ErrShareNotFound
	}
	return s.Queries.DeleteRankingShare(ctx, shareID)
}

func (s *ShareService) AcceptInvite(ctx context.Context, plaintextToken string, userID int64) (uuid.UUID, error) {
	hash := token.Hash(plaintextToken)

	invite, err := s.Queries.GetRankingInviteByTokenHash(ctx, hash)
	if err != nil {
		return uuid.UUID{}, ErrShareNotFound
	}

	if invite.InvitedUserID != nil {
		return uuid.UUID{}, ErrInviteAlreadyRedeemed
	}
	if invite.ExpiresAt.Valid && invite.ExpiresAt.Time.Before(time.Now()) {
		return uuid.UUID{}, ErrInviteExpired
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	txq := s.Queries.WithTx(tx)

	if err := txq.MarkRankingInviteRedeemed(ctx, db.MarkRankingInviteRedeemedParams{
		ID:            invite.ID,
		InvitedUserID: &userID,
	}); err != nil {
		return uuid.UUID{}, fmt.Errorf("redeem invite: %w", err)
	}

	if err := txq.UpdateRankingShareUserID(ctx, db.UpdateRankingShareUserIDParams{
		ID:     invite.RankingShareID,
		UserID: &userID,
	}); err != nil {
		return uuid.UUID{}, fmt.Errorf("update share user: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return uuid.UUID{}, fmt.Errorf("commit accept invite: %w", err)
	}

	share, err := s.Queries.GetRankingShareByID(ctx, invite.RankingShareID)
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("get share: %w", err)
	}

	ranking, err := s.Queries.GetRankingByID(ctx, share.RankingID)
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("get ranking: %w", err)
	}

	return ranking.Uuid, nil
}
