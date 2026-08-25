package services

import (
	"context"
	"fmt"

	"github.com/ghmeier/rankanything/internal/db"
)

type ListForUserRequest struct {
	UserID int64
}

type RankingSummary struct {
	Ranking   db.Ranking
	Published *db.RankingVersion
	Draft     *db.RankingVersion
}

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
			summary.Published = &v
		}
		if v, ok := draftByRanking[r.ID]; ok {
			summary.Draft = &v
		}
		summaries[i] = summary
	}
	return summaries, nil
}
