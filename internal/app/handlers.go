package app

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/ghmeier/rankanything/internal/auth"
	"github.com/ghmeier/rankanything/internal/constants"
	"github.com/ghmeier/rankanything/internal/db"
	"github.com/ghmeier/rankanything/internal/render"
	"github.com/ghmeier/rankanything/internal/services"
	"github.com/google/uuid"
)

// ─── Builder routes ─────────────────────────────────────────────────────────

// handleHome is GET / — a signed-in user goes straight to their rankings.
// A signed-out visitor sees a minimal holding page: feat/landing-page
// (wave 3) replaces it with the real marketing page. There is no anonymous
// entry point anymore — every ranking needs an owner from creation.
func (a *App) handleHome(w http.ResponseWriter, r *http.Request) {
	if userID := a.Sessions.UserID(r.Context()); userID != 0 {
		http.Redirect(w, r, "/me", http.StatusSeeOther)
		return
	}

	base := a.base(r)
	if err := a.Render.Page(w, http.StatusOK, "pages/home.html", HomeView{BaseView: base}); err != nil {
		a.serverError(w, r, err)
	}
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
	if render.IsHTMXRequest(r) {
		w.Header().Set("HX-Redirect", target)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// handleViewRanking is GET /r/{uuid} or GET /r/{uuid}/v/{short} — render the
// board for the version RequireRankingAccess resolved.
func (a *App) handleViewRanking(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rankingUUID := ctx.Value(constants.RankingUUIDKey).(uuid.UUID)
	version := ctx.Value(constants.RankingVersionKey).(db.RankingVersion)

	ranking, err := a.RankingSvc.GetRanking(ctx, rankingUUID)
	if err != nil {
		rankError(a, w, r, err)
		return
	}

	board, err := a.RankingSvc.GetBoard(ctx, ranking, version)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	if err := a.RenderRankingPage(w, r, board); err != nil {
		a.serverError(w, r, err)
	}
}

// handleUpdateRanking is POST /r/{uuid} — update title or description.
func (a *App) handleUpdateRanking(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rankingUUID := ctx.Value(constants.RankingUUIDKey).(uuid.UUID)

	updated, err := a.RankingSvc.UpdateRanking(ctx, services.UpdateRankingRequest{
		UUID:        rankingUUID,
		Name:        r.FormValue("title"),
		Description: r.FormValue("description"),
	})
	if err != nil {
		rankError(a, w, r, err)
		return
	}

	view := RankingView{Ranking: updated}
	if err := a.Render.Partial(w, http.StatusOK, "partials/ranking_meta.html", view); err != nil {
		a.serverError(w, r, err)
	}
}

// handleAddItem is POST /r/{uuid}/items — add a new item to the version
// being viewed.
func (a *App) handleAddItem(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rankingUUID := ctx.Value(constants.RankingUUIDKey).(uuid.UUID)
	version := ctx.Value(constants.RankingVersionKey).(db.RankingVersion)
	title := strings.TrimSpace(r.FormValue("label"))

	if title == "" {
		a.Render.Empty(w, http.StatusBadRequest)
		return
	}

	item, err := a.RankingSvc.AddItem(ctx, services.AddItemRequest{
		VersionID:      version.ID,
		Title:          title,
		ImageSourceURL: r.FormValue("image_url"),
	})
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	if err = a.Render.Partial(w, http.StatusOK, "partials/item_card.html", RankingItemCard{Item: item, RankingUUID: rankingUUID}); err != nil {
		a.serverError(w, r, err)
	}
}

// handleDeleteItem is DELETE /r/{uuid}/items/{itemID}.
func (a *App) handleDeleteItem(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	version := ctx.Value(constants.RankingVersionKey).(db.RankingVersion)

	id, err := strconv.ParseInt(r.PathValue("itemID"), 10, 64)
	if err != nil {
		a.notFound(w, r)
		return
	}

	if err := a.RankingSvc.DeleteItem(ctx, services.DeleteItemRequest{VersionID: version.ID, ItemID: id}); err != nil {
		rankError(a, w, r, err)
		return
	}

	a.Render.Empty(w, http.StatusAccepted)
}

// handleAddTier is POST /r/{uuid}/tiers — add a new tier.
func (a *App) handleAddTier(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rankingUUID := ctx.Value(constants.RankingUUIDKey).(uuid.UUID)
	version := ctx.Value(constants.RankingVersionKey).(db.RankingVersion)

	tier, err := a.RankingSvc.AddTier(ctx, services.AddTierRequest{
		VersionID: version.ID,
		RankingID: version.RankingID,
		Title:     r.FormValue("label"),
		Color:     r.FormValue("color"),
	})
	if err != nil {
		rankError(a, w, r, err)
		return
	}

	tierView := TierRowView{Tier: tier, RankingUUID: rankingUUID}
	if err := a.Render.Partial(w, http.StatusOK, "partials/tier_row.html", tierView); err != nil {
		a.serverError(w, r, err)
	}
}

// handleEditTier is POST /r/{uuid}/tiers/{tierID}/edit — enable editing a tier.
func (a *App) handleEditTier(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rankingUUID := ctx.Value(constants.RankingUUIDKey).(uuid.UUID)
	version := ctx.Value(constants.RankingVersionKey).(db.RankingVersion)

	id, err := strconv.ParseInt(r.PathValue("tierID"), 10, 64)
	if err != nil {
		a.notFound(w, r)
		return
	}

	tier, err := a.RankingSvc.GetTier(ctx, services.GetTierRequest{VersionID: version.ID, TierID: id})
	if err != nil {
		rankError(a, w, r, err)
		return
	}

	view := TierRowLabelView{Tier: tier, RankingUUID: rankingUUID}
	if err := a.Render.Partial(w, http.StatusAccepted, "partials/tier_row_label_editable.html", view); err != nil {
		a.serverError(w, r, err)
	}
}

// handleUpdateTier is PUT /r/{uuid}/tiers/{tierID} — rename or recolor.
func (a *App) handleUpdateTier(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rankingUUID := ctx.Value(constants.RankingUUIDKey).(uuid.UUID)
	version := ctx.Value(constants.RankingVersionKey).(db.RankingVersion)

	id, err := strconv.ParseInt(r.PathValue("tierID"), 10, 64)
	if err != nil {
		a.notFound(w, r)
		return
	}

	tier, err := a.RankingSvc.UpdateTier(ctx, services.UpdateTierRequest{
		VersionID: version.ID,
		TierID:    id,
		Title:     r.FormValue("label"),
		Color:     r.FormValue("color"),
	})
	if err != nil {
		rankError(a, w, r, err)
		return
	}

	view := TierRowLabelView{Tier: tier, RankingUUID: rankingUUID}
	if err := a.Render.Partial(w, http.StatusOK, "partials/tier_row_label.html", view); err != nil {
		a.serverError(w, r, err)
	}
}

// handleDeleteTier is DELETE /r/{uuid}/tiers/{tierID} — remove a tier. Its
// items return to the unassigned tray rather than being deleted.
func (a *App) handleDeleteTier(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	version := ctx.Value(constants.RankingVersionKey).(db.RankingVersion)

	id, err := strconv.ParseInt(r.PathValue("tierID"), 10, 64)
	if err != nil {
		a.notFound(w, r)
		return
	}

	if err := a.RankingSvc.DeleteTier(ctx, services.DeleteTierRequest{VersionID: version.ID, TierID: id}); err != nil {
		rankError(a, w, r, err)
		return
	}

	a.Render.Empty(w, http.StatusAccepted)
}

// handleAddItemToTier is POST /r/{uuid}/tiers/{tierID}/items — place an item
// in a tier, via drag-and-drop.
func (a *App) handleAddItemToTier(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rankingUUID := ctx.Value(constants.RankingUUIDKey).(uuid.UUID)
	version := ctx.Value(constants.RankingVersionKey).(db.RankingVersion)

	tierID, err := strconv.ParseInt(r.PathValue("tierID"), 10, 64)
	if err != nil {
		a.notFound(w, r)
		return
	}
	itemID, err := strconv.ParseInt(r.FormValue("item_id"), 10, 64)
	if err != nil {
		a.notFound(w, r)
		return
	}

	item, err := a.RankingSvc.AddItemToTier(ctx, services.AddItemToTierRequest{
		VersionID: version.ID,
		TierID:    tierID,
		ItemID:    itemID,
	})
	if err != nil {
		if errors.Is(err, services.ErrInvalidTierPlacement) {
			a.Render.Empty(w, http.StatusConflict)
			return
		}
		rankError(a, w, r, err)
		return
	}

	if err = a.Render.Partial(w, http.StatusOK, "partials/item_card.html", RankingItemCard{Item: item, RankingUUID: rankingUUID}); err != nil {
		a.serverError(w, r, err)
	}
}

// ─── Auth routes ─────────────────────────────────────────────────────────────

// handleRegisterForm is GET /register.
func (a *App) handleRegisterForm(w http.ResponseWriter, r *http.Request) {
	base := a.base(r)
	next := r.URL.Query().Get("next")
	view := AuthView{BaseView: base, Next: next}
	a.Render.Page(w, http.StatusOK, "pages/register.html", view)
}

// handleRegister is POST /register.
func (a *App) handleRegister(w http.ResponseWriter, r *http.Request) {
	next := r.FormValue("next")
	email, err := auth.NormalizeEmail(r.FormValue("email"))
	if err != nil {
		a.renderRegisterError(w, r, email, next, err.Error())
		return
	}
	password, err := auth.HashPassword(r.FormValue("password"))
	if err != nil {
		a.renderRegisterError(w, r, email, next, err.Error())
		return
	}

	_, err = a.UserSvc.Register(r.Context(), services.RegisterRequest{
		Email:    email,
		Password: password,
		Next:     next,
	})
	if err != nil {
		if errors.Is(err, services.ErrEmailAlreadyRegistered) {
			a.renderRegisterError(w, r, email, next, "email already registered")
			return
		}
		a.serverError(w, r, err)
		return
	}

	a.Sessions.Flash(r.Context(), "Account created!")

	// next is untrusted user input; only follow it when it stays on this
	// site, the same guard handleLogin uses, so registration can't be used
	// as an open redirect.
	target := "/me"
	if next != "" && isSiteRelativePath(next) {
		target = next
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (a *App) renderRegisterError(w http.ResponseWriter, r *http.Request, email, next, errMsg string) {
	base := a.base(r)
	view := AuthView{BaseView: base, Email: email, Next: next, Error: errMsg}
	a.Render.Page(w, http.StatusUnprocessableEntity, "pages/register.html", view)
}

// handleLoginForm is GET /login.
func (a *App) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	base := a.base(r)
	next := r.URL.Query().Get("next")
	view := AuthView{BaseView: base, Next: next}
	a.Render.Page(w, http.StatusOK, "pages/login.html", view)
}

// handleLogin is POST /login.
func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	next := r.FormValue("next")
	email, err := auth.NormalizeEmail(r.FormValue("email"))
	if err != nil {
		a.renderLoginError(w, r, email, next, err.Error())
		return
	}

	_, err = a.UserSvc.Login(r.Context(), services.LoginRequest{
		Email:    email,
		Password: r.FormValue("password"),
		Next:     next,
	})
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			a.renderLoginError(w, r, email, next, auth.ErrInvalidCredentials.Error())
			return
		}
		a.serverError(w, r, err)
		return
	}

	target := "/"
	if next != "" && isSiteRelativePath(next) {
		target = next
	}

	if render.IsHTMXRequest(r) {
		w.Header().Set("HX-Redirect", target)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// isSiteRelativePath reports whether next is safe to redirect to: a path on
// this site, not an absolute or protocol-relative URL that could send the
// user elsewhere (an open redirect). A leading "/\" is also rejected —
// some browsers normalize a backslash there into a second forward slash,
// turning what looks like a site-relative path into a protocol-relative one.
func isSiteRelativePath(next string) bool {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return false
	}
	return !strings.ContainsRune(next, '\\')
}

func (a *App) renderLoginError(w http.ResponseWriter, r *http.Request, email, next, errMsg string) {
	base := a.base(r)
	view := AuthView{BaseView: base, Email: email, Next: next, Error: errMsg}
	err := a.Render.Partial(w, http.StatusUnauthorized, "pages/login.html", view)
	if err != nil {
		a.serverError(w, r, err)
	}
}

// handleLogout is POST /logout.
func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	_ = a.UserSvc.Logout(r.Context())
	if render.IsHTMXRequest(r) {
		w.Header().Set("HX-Redirect", "/")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// rankError maps service errors to HTTP responses.
func rankError(a *App, w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, services.ErrRankingNotFound) {
		a.notFound(w, r)
		return
	}
	a.serverError(w, r, err)
}
