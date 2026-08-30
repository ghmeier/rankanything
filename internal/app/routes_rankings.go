package app

import "net/http"

func (a *App) registerRankingRoutes(mux *http.ServeMux) {
	mux.Handle("POST /new", a.RequireUser(http.HandlerFunc(a.handleNew)))
	mux.Handle("GET /me", a.RequireUser(http.HandlerFunc(a.handleRankingsIndex)))

	mutable := func(h http.HandlerFunc) http.Handler {
		return a.RequireRankingAccess(a.requireDraftVersion(h))
	}

	mux.Handle("GET /r/{uuid}", a.RequireRankingAccess(http.HandlerFunc(a.handleViewRanking)))
	mux.Handle("GET /r/{uuid}/v/{short}", a.RequireRankingAccess(http.HandlerFunc(a.handleViewRanking)))
	mux.Handle("GET /r/{uuid}/export", a.RequireRankingAccess(http.HandlerFunc(a.handleExportBoard)))
	mux.Handle("GET /r/{uuid}/v/{short}/export", a.RequireRankingAccess(http.HandlerFunc(a.handleExportBoard)))
	mux.Handle("POST /r/{uuid}", a.RequireRankingAccess(http.HandlerFunc(a.handleUpdateRanking)))

	mux.Handle("GET /r/{uuid}/description", a.RequireRankingAccess(http.HandlerFunc(a.handleViewDescription)))
	mux.Handle("GET /r/{uuid}/description/edit", a.RequireRankingAccess(http.HandlerFunc(a.handleEditDescription)))
	mux.Handle("POST /r/{uuid}/description", a.RequireRankingAccess(http.HandlerFunc(a.handleUpdateDescription)))
	mux.Handle("POST /r/{uuid}/v/{short}/items", mutable(a.handleAddItem))
	mux.Handle("POST /r/{uuid}/v/{short}/items/upload", mutable(a.handleUploadItem))
	mux.Handle("GET /r/{uuid}/v/{short}/items/{itemID}", mutable(a.handleViewItem))
	mux.Handle("GET /r/{uuid}/v/{short}/items/{itemID}/edit", mutable(a.handleEditItem))
	mux.Handle("PUT /r/{uuid}/v/{short}/items/{itemID}", mutable(a.handleUpdateItem))
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

	mux.Handle("GET /r/{uuid}/share", a.RequireRankingAccess(http.HandlerFunc(a.handleGetShareModal)))
	mux.Handle("POST /r/{uuid}/share/link", a.RequireRankingAccess(http.HandlerFunc(a.handleEnableShare)))
	mux.Handle("DELETE /r/{uuid}/share/link", a.RequireRankingAccess(http.HandlerFunc(a.handleDisableShare)))
	mux.Handle("POST /r/{uuid}/share/invites", a.RequireRankingAccess(http.HandlerFunc(a.handleInviteByEmail)))
	mux.Handle("DELETE /r/{uuid}/share/invites/{shareID}", a.RequireRankingAccess(http.HandlerFunc(a.handleRevokeShare)))
}
