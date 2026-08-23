package app

import "net/http"

// registerRankingRoutes mounts the signed-in rankings surface: the landing
// placeholder, the account page, and the board plus its mutating endpoints,
// all gated by RequireRankingAccess. feat/rankings-index and
// feat/versioned-board (wave 3) build out the handlers behind these routes.
func (a *App) registerRankingRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", a.handleHome)
	mux.Handle("POST /new", a.RequireUser(http.HandlerFunc(a.handleNew)))
	mux.Handle("GET /me", a.RequireUser(http.HandlerFunc(a.handleMe)))

	mux.Handle("GET /r/{uuid}", a.RequireRankingAccess(http.HandlerFunc(a.handleViewRanking)))
	mux.Handle("GET /r/{uuid}/v/{short}", a.RequireRankingAccess(http.HandlerFunc(a.handleViewRanking)))
	mux.Handle("POST /r/{uuid}", a.RequireRankingAccess(http.HandlerFunc(a.handleUpdateRanking)))
	mux.Handle("POST /r/{uuid}/items", a.RequireRankingAccess(http.HandlerFunc(a.handleAddItem)))
	mux.Handle("DELETE /r/{uuid}/items/{itemID}", a.RequireRankingAccess(http.HandlerFunc(a.handleDeleteItem)))
	mux.Handle("POST /r/{uuid}/tiers", a.RequireRankingAccess(http.HandlerFunc(a.handleAddTier)))
	mux.Handle("PUT /r/{uuid}/tiers/{tierID}", a.RequireRankingAccess(http.HandlerFunc(a.handleUpdateTier)))
	mux.Handle("DELETE /r/{uuid}/tiers/{tierID}", a.RequireRankingAccess(http.HandlerFunc(a.handleDeleteTier)))
	mux.Handle("POST /r/{uuid}/tiers/{tierID}/edit", a.RequireRankingAccess(http.HandlerFunc(a.handleEditTier)))
	mux.Handle("POST /r/{uuid}/tiers/{tierID}/items", a.RequireRankingAccess(http.HandlerFunc(a.handleAddItemToTier)))
}
