package app

import (
	"net/http"
	"strings"

	"github.com/ghmeier/rankanything/internal/constants"
	"github.com/ghmeier/rankanything/internal/db"
	"github.com/ghmeier/rankanything/internal/services"
	"github.com/ghmeier/rankanything/internal/ui"
	"github.com/google/uuid"
)

func (a *App) handleGetShareModal(w http.ResponseWriter, r *http.Request) {
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

	link, err := a.ShareSvc.GetLinkShare(ctx, ranking.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	shares, err := a.ShareSvc.ListEmailShares(ctx, ranking.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	props := shareModalProps(rankingUUID.String(), validation, link, shares)
	a.render(w, r, http.StatusOK, ui.ShareModalContent(props))
}

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

	shares, err := a.ShareSvc.ListEmailShares(ctx, ranking.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	props := shareModalProps(rankingUUID.String(), validation, link, shares)
	a.render(w, r, http.StatusOK, ui.ShareModalBody(props))
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

	shares, err := a.ShareSvc.ListEmailShares(ctx, ranking.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	props := shareModalProps(rankingUUID.String(), validation, services.LinkShare{}, shares)
	a.render(w, r, http.StatusOK, ui.ShareModalBody(props))
}

func (a *App) handleInviteByEmail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rankingUUID := ctx.Value(constants.RankingUUIDKey).(uuid.UUID)

	email := strings.TrimSpace(r.FormValue("email"))
	role := db.RankingShareRole(r.FormValue("role"))
	if email == "" || (role != db.RankingShareRoleREADER && role != db.RankingShareRoleEDITOR) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	ranking, err := a.RankingSvc.GetRanking(ctx, rankingUUID)
	if err != nil {
		a.rankError(w, r, err)
		return
	}

	inviter, err := a.Queries.GetUserByID(ctx, a.Sessions.UserID(ctx))
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	if _, err := a.ShareSvc.InviteByEmail(ctx, services.InviteRequest{
		RankingID:     ranking.ID,
		Email:         email,
		Role:          role,
		InviterUserID: inviter.ID,
		InviterName:   inviter.Email,
		RankingName:   ranking.Name,
	}); err != nil {
		a.serverError(w, r, err)
		return
	}

	validation, err := a.ShareSvc.ValidateShareable(ctx, ranking)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	link, err := a.ShareSvc.GetLinkShare(ctx, ranking.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	shares, err := a.ShareSvc.ListEmailShares(ctx, ranking.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	props := shareModalProps(rankingUUID.String(), validation, link, shares)
	a.render(w, r, http.StatusOK, ui.ShareModalBody(props))
}

func (a *App) handleRevokeShare(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rankingUUID := ctx.Value(constants.RankingUUIDKey).(uuid.UUID)

	shareID, ok := a.pathID(w, r, "shareID")
	if !ok {
		return
	}

	ranking, err := a.RankingSvc.GetRanking(ctx, rankingUUID)
	if err != nil {
		a.rankError(w, r, err)
		return
	}

	if err := a.ShareSvc.RemoveShare(ctx, shareID, ranking.ID); err != nil {
		a.serverError(w, r, err)
		return
	}

	validation, err := a.ShareSvc.ValidateShareable(ctx, ranking)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	link, err := a.ShareSvc.GetLinkShare(ctx, ranking.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	shares, err := a.ShareSvc.ListEmailShares(ctx, ranking.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	props := shareModalProps(rankingUUID.String(), validation, link, shares)
	a.render(w, r, http.StatusOK, ui.ShareModalBody(props))
}

func (a *App) handleAcceptInvite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := r.PathValue("token")
	userID := a.Sessions.UserID(ctx)

	rankingUUID, err := a.ShareSvc.AcceptInvite(ctx, token, userID)
	if err != nil {
		a.Sessions.Flash(ctx, err.Error())
		redirect(w, r, "/me")
		return
	}

	a.Sessions.Flash(ctx, "Invitation accepted")
	redirect(w, r, "/r/"+rankingUUID.String())
}
