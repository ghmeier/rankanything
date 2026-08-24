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

// Covers an unknown slug and a revoked one alike, so a 404 never leaks which
// slugs have existed.
var ErrShareNotPublic = errors.New("Share not found.")

type ShareService struct {
	Queries *db.Queries

	// BaseURL is this site's absolute origin, for building copyable links.
	BaseURL string
}

// LinkShare is the "anyone with the link" row's state. Never-shared and
// revoked both report the zero value.
type LinkShare struct {
	IsPublic bool
	URL      string
}

// ShareGate lists every missing condition at once, not just the first.
type ShareGate struct {
	Shareable bool
	Reasons   []string
}

// EvaluateShareGate wants a published version and a verified owner email.
func (s *ShareService) EvaluateShareGate(ctx context.Context, ranking db.Ranking) (ShareGate, error) {
	versions, err := s.Queries.ListRankingVersionsForRanking(ctx, ranking.ID)
	if err != nil {
		return ShareGate{}, err
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
		return ShareGate{}, err
	}

	var reasons []string
	if !hasPublished {
		reasons = append(reasons, "Publish at least one version.")
	}
	if !owner.EmailVerified {
		reasons = append(reasons, "Verify your email.")
	}
	return ShareGate{Shareable: len(reasons) == 0, Reasons: reasons}, nil
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

// EnableLinkShare mints a fresh public_slug. The caller checks ShareGate.
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
		// Retrying is insurance against a slug collision; 80 bits of
		// randomness means this loop rarely runs twice.
		if !isUniqueViolation(err) {
			return LinkShare{}, err
		}
		lastErr = err
	}
	return LinkShare{}, fmt.Errorf("mint public slug: %w", lastErr)
}

// DisableLinkShare kills the old link for good; nothing keeps the cleared
// slug around to be reused.
func (s *ShareService) DisableLinkShare(ctx context.Context, rankingID int64) error {
	return s.Queries.ClearRankingPublicSlug(ctx, rankingID)
}

type PublicRanking struct {
	Ranking db.Ranking
	Version db.RankingVersion
}

// ResolvePublicRanking fails identically for an unknown slug, a revoked one,
// and one whose ranking has nothing published.
func (s *ShareService) ResolvePublicRanking(ctx context.Context, slug string) (PublicRanking, error) {
	share, err := s.Queries.GetRankingShareByPublicSlug(ctx, &slug)
	if err != nil || !share.IsPublic {
		return PublicRanking{}, ErrShareNotPublic
	}

	ranking, err := s.Queries.GetRankingByID(ctx, share.RankingID)
	if err != nil {
		return PublicRanking{}, ErrShareNotPublic
	}

	// A ranking with only a draft resolves to that draft, which is exactly
	// what must not be exposed publicly.
	version, err := s.Queries.ResolveLiveRankingVersion(ctx, ranking.ID)
	if err != nil || !version.PublishedAt.Valid {
		return PublicRanking{}, ErrShareNotPublic
	}

	return PublicRanking{Ranking: ranking, Version: version}, nil
}

func (s *ShareService) PublicURL(slug string) string {
	return strings.TrimRight(s.BaseURL, "/") + "/s/" + slug
}

// newPublicSlug takes more entropy than newShortUUID because a public slug
// must be unguessable across every ranking, not unique within one.
func newPublicSlug() (string, error) {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate public slug: %w", err)
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)), nil
}
