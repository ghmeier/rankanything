package app

import (
	"net/http"
	"strings"

	"github.com/ghmeier/rankanything/internal/constants"
	"github.com/ghmeier/rankanything/internal/services"
	"github.com/ghmeier/rankanything/internal/ui"
	"github.com/google/uuid"
)

// handleEnableShare re-derives the share gate rather than trusting the
// client, since a stale page can reach here after the gate closes.
func (a *App) handleEnableShare(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rankingUUID := ctx.Value(constants.RankingUUIDKey).(uuid.UUID)

	ranking, err := a.RankingSvc.GetRanking(ctx, rankingUUID)
	if err != nil {
		a.rankError(w, r, err)
		return
	}

	gate, err := a.ShareSvc.EvaluateShareGate(ctx, ranking)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if !gate.Shareable {
		a.forbidden(w, r, strings.Join(gate.Reasons, " "))
		return
	}

	link, err := a.ShareSvc.EnableLinkShare(ctx, ranking.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	props := shareControlProps(rankingUUID.String(), gate, link)
	props.Open = true
	a.render(w, r, http.StatusOK, ui.ShareControl(props))
}

// handleDisableShare kills the old link for good; re-sharing mints a new one.
func (a *App) handleDisableShare(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rankingUUID := ctx.Value(constants.RankingUUIDKey).(uuid.UUID)

	ranking, err := a.RankingSvc.GetRanking(ctx, rankingUUID)
	if err != nil {
		a.rankError(w, r, err)
		return
	}

	if err := a.ShareSvc.DisableLinkShare(ctx, ranking.ID); err != nil {
		a.serverError(w, r, err)
		return
	}

	gate, err := a.ShareSvc.EvaluateShareGate(ctx, ranking)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	props := shareControlProps(rankingUUID.String(), gate, services.LinkShare{})
	props.Open = true
	a.render(w, r, http.StatusOK, ui.ShareControl(props))
}
