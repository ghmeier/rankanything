package services

import (
	"context"
	"fmt"

	"github.com/ghmeier/rankanything/internal/db"
)

// ListForUserRequest is the input for listing a user's rankings for the
// rankings index.
type ListForUserRequest struct {
	UserID int64
}

// RankingSummary pairs a ranking with the version information its rankings
// index card needs. A ranking can have both a published version and a newer
// draft sitting on top of it at once, so both are carried separately rather
// than collapsing to a single "live version" — the card needs to say "the
// last publish is from Aug 4, and there's a draft in progress" rather than
// picking one fact and hiding the other.
type RankingSummary struct {
	Ranking   db.Ranking
	Published *db.RankingVersion
	Draft     *db.RankingVersion
}

// ListForUser fetches every ranking a user owns, together with the version
// information each card describes: the ranking's most recently published
// version, if any, and its draft, if any. This runs in three queries total
// no matter how many rankings the user has — the rankings themselves, the
// latest published version per ranking (one row per ranking via
// DISTINCT ON), and the at-most-one draft per ranking — rather than the one
// query per ranking a naive per-card lookup would issue.
func (s *RankingsService) ListForUser(ctx context.Context, req ListForUserRequest) ([]RankingSummary, error) {
	rankings, err := s.Queries.ListRankingsByUser(ctx, req.UserID)
	if err != nil {
		return nil, fmt.Errorf("list rankings: %w", err)
	}
	if len(rankings) == 0 {
		return nil, nil
	}

	rankingIDs := make([]int64, len(rankings))
	for i, r := range rankings {
		rankingIDs[i] = r.ID
	}

	published, err := s.Queries.ListLatestPublishedRankingVersionsForRankings(ctx, rankingIDs)
	if err != nil {
		return nil, fmt.Errorf("list published versions: %w", err)
	}
	publishedByRanking := make(map[int64]db.RankingVersion, len(published))
	for _, v := range published {
		publishedByRanking[v.RankingID] = v
	}

	drafts, err := s.Queries.ListDraftRankingVersionsForRankings(ctx, rankingIDs)
	if err != nil {
		return nil, fmt.Errorf("list draft versions: %w", err)
	}
	draftByRanking := make(map[int64]db.RankingVersion, len(drafts))
	for _, v := range drafts {
		draftByRanking[v.RankingID] = v
	}

	summaries := make([]RankingSummary, len(rankings))
	for i, r := range rankings {
		summary := RankingSummary{Ranking: r}
		if v, ok := publishedByRanking[r.ID]; ok {
			v := v
			summary.Published = &v
		}
		if v, ok := draftByRanking[r.ID]; ok {
			v := v
			summary.Draft = &v
		}
		summaries[i] = summary
	}
	return summaries, nil
}
