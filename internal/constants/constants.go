package constants

type contextKey string

const (
	// RankingUUIDKey stores the ranking's external identifier (rankings.uuid),
	// parsed from the request path, once RequireRankingAccess has confirmed the
	// session's user owns it.
	RankingUUIDKey contextKey = "ranking_uuid"
	// RankingVersionKey stores the db.RankingVersion RequireRankingAccess
	// resolved for this request: the live version for /r/{uuid}, or the one
	// pinned by /r/{uuid}/v/{short}.
	RankingVersionKey contextKey = "ranking_version"
)
