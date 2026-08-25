package services

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"

	"github.com/ghmeier/rankanything/internal/db"
)

var ErrShareNotPublic = errors.New("Share not found.")

type ShareService struct {
	Queries *db.Queries

	BaseURL string
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
