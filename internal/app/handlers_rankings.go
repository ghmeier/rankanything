package app

import (
	"fmt"
	"net/http"

	"github.com/ghmeier/rankanything/internal/db"
	"github.com/ghmeier/rankanything/internal/services"
	"github.com/ghmeier/rankanything/internal/ui"
)

// handleRankingsIndex is GET /me — the page a signed-in user lands on after
// logging in or registering, listing every ranking they own. RequireUser
// gates this route, so a signed-out request never reaches here.
//
// It renders directly through templ rather than internal/render, the same
// way ui.ComponentsHandler does — this page has no htmx fragment swaps of
// its own, so it doesn't need the page/partial dispatch internal/render
// exists for.
func (a *App) handleRankingsIndex(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := a.Sessions.UserID(ctx)

	summaries, err := a.RankingSvc.ListForUser(ctx, services.ListForUserRequest{UserID: userID})
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	base := a.base(r)
	props := ui.RankingsIndexProps{
		CSRFToken: base.CSRFToken,
		LoggedIn:  base.User != nil,
		Flash:     base.Flash,
		Rankings:  make([]ui.RankingsIndexCard, len(summaries)),
	}
	if base.User != nil && !base.User.EmailVerified {
		props.VerificationNotice = ui.ResendVerificationNotice(base.User.Email)
	}
	for i, summary := range summaries {
		props.Rankings[i] = ui.RankingsIndexCard{
			URL:    "/r/" + summary.Ranking.Uuid.String(),
			Name:   summary.Ranking.Name,
			Status: rankingStatusText(summary),
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := ui.RankingsIndexPage(props).Render(ctx, w); err != nil {
		a.serverError(w, r, err)
	}
}

// rankingStatusText describes a ranking's live state the way its rankings
// index card needs to. A ranking can have a published version and a newer
// draft at the same time, and that combination has to say so explicitly —
// "published Aug 4 · draft in progress" — rather than collapsing to
// whichever version happens to be "live", because a user checking the
// index needs to know a publish is stale before they decide whether to
// open the board.
func rankingStatusText(summary services.RankingSummary) string {
	switch {
	case summary.Published != nil && summary.Draft != nil:
		return fmt.Sprintf("published %s · draft in progress", formatPublishedAt(*summary.Published))
	case summary.Published != nil:
		return fmt.Sprintf("published %s", formatPublishedAt(*summary.Published))
	default:
		return "draft"
	}
}

func formatPublishedAt(v db.RankingVersion) string {
	if !v.PublishedAt.Valid {
		return ""
	}
	return v.PublishedAt.Time.Format("Jan 2")
}

// handleNew is POST /new — create a fresh ranking for the signed-in user,
// with its draft version seeded from the default tier palette. RequireUser
// gates it, so the signed-out check lives there rather than being repeated
// here. It's a POST rather than the prototype's GET because a GET carries no
// CSRF check (the middleware deliberately skips GET/HEAD/OPTIONS) and is
// fair game for link prefetch or a stray navigation — neither should mint a
// ranking.
func (a *App) handleNew(w http.ResponseWriter, r *http.Request) {
	userID := a.Sessions.UserID(r.Context())

	ranking, err := a.RankingSvc.CreateForUser(r.Context(), services.CreateForUserRequest{UserID: userID})
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	target := "/r/" + ranking.Uuid.String()
	if isHTMXRequest(r) {
		w.Header().Set("HX-Redirect", target)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}
