package app

import (
	"net/http"
	"strings"

	"github.com/ghmeier/rankanything/internal/constants"
	"github.com/ghmeier/rankanything/internal/services"
	"github.com/ghmeier/rankanything/internal/ui"
	"github.com/google/uuid"
)

// handleEnableShare is POST /r/{uuid}/share — turn on public sharing for
// the ranking, minting a fresh public_slug. It re-derives ShareGate itself
// rather than trusting the client: the board only shows this control's
// enable action when the gate already passed, but a stale page or a direct
// request could still reach here after the gate flips back closed.
func (a *App) handleEnableShare(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rankingUUID := ctx.Value(constants.RankingUUIDKey).(uuid.UUID)

	ranking, err := a.RankingSvc.GetRanking(ctx, rankingUUID)
	if err != nil {
		rankError(a, w, r, err)
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
	if err := renderComponent(w, r, http.StatusOK, ui.ShareControl(props)); err != nil {
		a.serverError(w, r, err)
	}
}

// handleDisableShare is DELETE /r/{uuid}/share — turn off public sharing
// and clear the slug. The old link is dead permanently: re-sharing later
// mints a different one.
func (a *App) handleDisableShare(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rankingUUID := ctx.Value(constants.RankingUUIDKey).(uuid.UUID)

	ranking, err := a.RankingSvc.GetRanking(ctx, rankingUUID)
	if err != nil {
		rankError(a, w, r, err)
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
	if err := renderComponent(w, r, http.StatusOK, ui.ShareControl(props)); err != nil {
		a.serverError(w, r, err)
	}
}
