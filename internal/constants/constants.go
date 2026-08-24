package constants

type contextKey string

const (
	// Set by RequireRankingAccess once it confirms the session owns the
	// ranking; the version is the live one, or the one the path pinned.
	RankingUUIDKey    contextKey = "ranking_uuid"
	RankingVersionKey contextKey = "ranking_version"
)
