package app

import (
	"fmt"
	"net/http"

	"github.com/ghmeier/rankanything/internal/db"
	"github.com/ghmeier/rankanything/internal/services"
	"github.com/ghmeier/rankanything/internal/ui"
)

// handleRankingsIndex lists the rankings a user owns. RequireUser gates it.
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
		Theme:     base.Theme,
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

	a.render(w, r, http.StatusOK, ui.RankingsIndexPage(props))
}

// rankingStatusText names both versions when a draft sits on a publish, so a
// reader can tell the live version is stale before opening the board.
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

// handleNew is a POST because a GET skips the CSRF check and is fair game for
// link prefetch, and neither should mint a ranking.
func (a *App) handleNew(w http.ResponseWriter, r *http.Request) {
	userID := a.Sessions.UserID(r.Context())

	ranking, err := a.RankingSvc.CreateForUser(r.Context(), services.CreateForUserRequest{UserID: userID})
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	redirect(w, r, "/r/"+ranking.Uuid.String())
}
