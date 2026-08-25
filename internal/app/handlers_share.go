package app

import (
	"net/http"
	"strings"

	"github.com/ghmeier/rankanything/internal/constants"
	"github.com/ghmeier/rankanything/internal/services"
	"github.com/ghmeier/rankanything/internal/ui"
	"github.com/google/uuid"
)

func (a *App) handleEnableShare(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rankingUUID := ctx.Value(constants.RankingUUIDKey).(uuid.UUID)

	ranking, err := a.RankingSvc.GetRanking(ctx, rankingUUID)
	if err != nil {
		a.rankError(w, r, err)
		return
	}

	validation, err := a.ShareSvc.ValidateShareable(ctx, ranking)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if !validation.Shareable {
		a.forbidden(w, r, strings.Join(validation.Reasons, " "))
		return
	}

	link, err := a.ShareSvc.EnableLinkShare(ctx, ranking.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	props := shareControlProps(rankingUUID.String(), validation, link)
	props.Open = true
	a.render(w, r, http.StatusOK, ui.ShareControl(props))
}

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

	validation, err := a.ShareSvc.ValidateShareable(ctx, ranking)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	props := shareControlProps(rankingUUID.String(), validation, services.LinkShare{})
	props.Open = true
	a.render(w, r, http.StatusOK, ui.ShareControl(props))
}
