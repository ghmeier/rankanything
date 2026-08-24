package app

import (
	"net/http"

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
		card := ui.RankingsIndexCard{
			URL:  "/r/" + summary.Ranking.Uuid.String(),
			Name: summary.Ranking.Name,
		}

		// A ranking that has never been published has no Published version,
		// and one whose last draft is published has no Draft; a ranking with
		// a draft on top of a publish has both.
		if summary.Published != nil {
			card.PublishedAt = services.FormatPublishedAt(*summary.Published)
		}
		if summary.Draft != nil {
			card.DraftURL = card.URL + "/v/" + summary.Draft.ShortUuid
		}

		props.Rankings[i] = card
	}

	a.render(w, r, http.StatusOK, ui.RankingsIndexPage(props))
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
