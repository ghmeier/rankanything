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

// ErrShareNotPublic covers every reason a public_slug fails to resolve to a
// live share: unknown, or revoked. A handler can't tell those apart from
// the error alone — same as ErrRankingNotFound — so a public route answers
// with a plain 404 either way rather than leaking which slugs ever existed.
var ErrShareNotPublic = errors.New("Share not found.")

// ShareService owns public-link sharing: toggling ranking_shares.is_public,
// minting and clearing public_slug, and resolving a public_slug back to the
// ranking and version it shares.
type ShareService struct {
	Queries *db.Queries

	// BaseURL is the site's own absolute origin (e.g.
	// "https://rankanything.app"), used to build the full copyable link a
	// share mints. Mirrors VerificationService.BaseURL.
	BaseURL string
}

// LinkShare is the "anyone with the link" ranking_shares row's public
// state. A ranking with no such row yet (never shared) and one that has
// been revoked report the same zero value — neither has a live link.
type LinkShare struct {
	IsPublic bool
	URL      string
}

// ShareGate reports whether a ranking is eligible to mint a public link,
// and which condition is missing when it isn't — the two are independent,
// so a caller sees every reason at once rather than just the first one
// evaluated. Mirrors RankingsService.PublishGate.
type ShareGate struct {
	Shareable bool
	Reasons   []string
}

// EvaluateShareGate computes a ranking's ShareGate: at least one published
// version, and the owner's email verified.
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
		reasons = append(reasons, "Publish a version before sharing a link.")
	}
	if !owner.EmailVerified {
		reasons = append(reasons, "Verify your email before sharing a link.")
	}
	return ShareGate{Shareable: len(reasons) == 0, Reasons: reasons}, nil
}

// GetLinkShare reports the current public-link state for a ranking.
func (s *ShareService) GetLinkShare(ctx context.Context, rankingID int64) (LinkShare, error) {
	shares, err := s.Queries.ListRankingSharesForRanking(ctx, rankingID)
	if err != nil {
		return LinkShare{}, err
	}
	for _, share := range shares {
		// The link share is the row with neither a user nor an email
		// attached — the only kind ranking_shares holds until invites
		// (RankingInvite, the OWNER/EDITOR roles) are built.
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

// EnableLinkShare turns on public sharing for a ranking, minting a fresh
// public_slug. It does not itself check ShareGate — the caller is expected
// to have already, the same way PublishVersion is the one that checks
// PublishGate rather than every layer above it re-deriving the same
// question.
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
		// newPublicSlug draws from 80 bits of randomness, so a collision on
		// ranking_shares_public_slug_idx is theoretical insurance rather
		// than a path this loop expects to exercise; anything else fails
		// immediately rather than retrying a real error.
		if !isUniqueViolation(err) {
			return LinkShare{}, err
		}
		lastErr = err
	}
	return LinkShare{}, fmt.Errorf("mint public slug: %w", lastErr)
}

// DisableLinkShare turns off public sharing and clears the slug. The old
// link is dead for good: GetRankingShareByPublicSlug looks a share up by
// the slug's own value, and nothing keeps the cleared one around to be
// reused.
func (s *ShareService) DisableLinkShare(ctx context.Context, rankingID int64) error {
	return s.Queries.ClearRankingPublicSlug(ctx, rankingID)
}

// PublicRanking is what the read-only public route needs: the ranking a
// slug shares and that ranking's live published version.
type PublicRanking struct {
	Ranking db.Ranking
	Version db.RankingVersion
}

// ResolvePublicRanking resolves a public_slug to the ranking it shares and
// that ranking's live published version. It fails the same way for an
// unknown slug, a revoked one, and one whose ranking currently has nothing
// published — the public route answers all three with a 404.
func (s *ShareService) ResolvePublicRanking(ctx context.Context, slug string) (PublicRanking, error) {
	share, err := s.Queries.GetRankingShareByPublicSlug(ctx, &slug)
	if err != nil || !share.IsPublic {
		return PublicRanking{}, ErrShareNotPublic
	}

	ranking, err := s.Queries.GetRankingByID(ctx, share.RankingID)
	if err != nil {
		return PublicRanking{}, ErrShareNotPublic
	}

	// The live version is the most recently published one; a ranking whose
	// only version is still a draft resolves to that draft instead, which
	// is exactly what must not be exposed publicly.
	version, err := s.Queries.ResolveLiveRankingVersion(ctx, ranking.ID)
	if err != nil || !version.PublishedAt.Valid {
		return PublicRanking{}, ErrShareNotPublic
	}

	return PublicRanking{Ranking: ranking, Version: version}, nil
}

// PublicURL builds the full copyable link for a public_slug.
func (s *ShareService) PublicURL(slug string) string {
	return strings.TrimRight(s.BaseURL, "/") + "/s/" + slug
}

// newPublicSlug returns a 16-character lowercase identifier for a public
// share link. 10 random bytes, base32 encoded with no padding, draws more
// entropy than a ranking version's 8-character short_uuid (5 bytes) — a
// public link's slug has to be unguessable across every ranking that has
// ever existed, not just unique within one ranking's own versions.
func newPublicSlug() (string, error) {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate public slug: %w", err)
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)), nil
}
