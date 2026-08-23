package app

import "net/http"

// registerPublicRoutes will carry the read-only public view once
// feat/public-share (wave 4) lands at GET /s/{public_slug}. It exists now,
// deliberately empty, because that route must not run through
// RequireRankingAccess — a public link has different access rules and must
// carry no edit affordances.
func (a *App) registerPublicRoutes(mux *http.ServeMux) {}
