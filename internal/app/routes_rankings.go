package app

import "net/http"

// registerRankingRoutes mounts the signed-in rankings surface: the landing
// placeholder, the account page, and the board plus its mutating endpoints,
// all gated by RequireRankingAccess. feat/rankings-index and
// feat/versioned-board (wave 3) build out the handlers behind these routes.
//
// Every route that mutates a version's tiers, items, or placements is also
// wrapped in requireDraftVersion, since a published version is immutable.
// handleUpdateRanking is exempt — the ranking's title and description
// aren't version-scoped — and so is handleCreateVersion, which requires a
// published version to branch off of. mutable composes the two middlewares
// in the order requireDraftVersion needs: RequireRankingAccess resolves and
// stashes the version in context before requireDraftVersion reads it back
// out.
func (a *App) registerRankingRoutes(mux *http.ServeMux) {
	mux.Handle("POST /new", a.RequireUser(http.HandlerFunc(a.handleNew)))
	mux.Handle("GET /me", a.RequireUser(http.HandlerFunc(a.handleRankingsIndex)))

	mutable := func(h http.HandlerFunc) http.Handler {
		return a.RequireRankingAccess(a.requireDraftVersion(h))
	}

	mux.Handle("GET /r/{uuid}", a.RequireRankingAccess(http.HandlerFunc(a.handleViewRanking)))
	mux.Handle("GET /r/{uuid}/v/{short}", a.RequireRankingAccess(http.HandlerFunc(a.handleViewRanking)))
	mux.Handle("POST /r/{uuid}", a.RequireRankingAccess(http.HandlerFunc(a.handleUpdateRanking)))
	mux.Handle("POST /r/{uuid}/v/{short}/items", mutable(a.handleAddItem))
	mux.Handle("DELETE /r/{uuid}/v/{short}/items/{itemID}", mutable(a.handleDeleteItem))
	mux.Handle("POST /r/{uuid}/v/{short}/items/{itemID}/unrank", mutable(a.handleUnrankItem))
	mux.Handle("POST /r/{uuid}/v/{short}/tiers", mutable(a.handleAddTier))
	mux.Handle("POST /r/{uuid}/v/{short}/tiers/reorder", mutable(a.handleReorderTiers))
	mux.Handle("PUT /r/{uuid}/v/{short}/tiers/{tierID}", mutable(a.handleUpdateTier))
	mux.Handle("DELETE /r/{uuid}/v/{short}/tiers/{tierID}", mutable(a.handleDeleteTier))
	mux.Handle("POST /r/{uuid}/v/{short}/tiers/{tierID}/edit", mutable(a.handleEditTier))
	mux.Handle("POST /r/{uuid}/v/{short}/tiers/{tierID}/items", mutable(a.handleAddItemToTier))
	mux.Handle("POST /r/{uuid}/v/{short}/tiers/{tierID}/items/reorder", mutable(a.handleReorderTierItems))
	mux.Handle("POST /r/{uuid}/v/{short}/publish", mutable(a.handlePublishVersion))
	mux.Handle("POST /r/{uuid}/versions", a.RequireRankingAccess(http.HandlerFunc(a.handleCreateVersion)))
}
