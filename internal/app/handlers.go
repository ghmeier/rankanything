package app

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/ghmeier/rankanything/internal/auth"
	"github.com/ghmeier/rankanything/internal/db"
	"github.com/ghmeier/rankanything/internal/services"
	"github.com/google/uuid"
)

// ─── Builder routes ─────────────────────────────────────────────────────────

// handleHome is GET / — resume an existing draft or create a new one.
func (a *App) handleHome(w http.ResponseWriter, r *http.Request) {
	if userID := a.Sessions.UserID(r.Context()); userID != 0 {
		http.Redirect(w, r, "/rankings", http.StatusSeeOther)
		return
	}

	ranking, err := a.RankingSvc.EnsureDraft(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	http.Redirect(w, r, "/r/"+ranking.Slug.String(), http.StatusSeeOther)
}

// handleNew is GET /new — create a fresh ranking for a signed-in user.
func (a *App) handleNew(w http.ResponseWriter, r *http.Request) {
	_ = a.base(r)
	userID := a.Sessions.UserID(r.Context())
	if userID == 0 {
		http.Redirect(w, r, "/login?next=/new", http.StatusSeeOther)
		return
	}

	ranking, err := a.RankingSvc.CreateForUser(r.Context(), services.CreateForUserRequest{UserID: userID})
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/r/"+ranking.Slug.String(), http.StatusSeeOther)
}

// handleViewRanking is GET /r/{slug} — render the board for a ranking.
func (a *App) handleViewRanking(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slug := ctx.Value(auth.SlugKey).(uuid.UUID)

	boardData, err := a.RankingSvc.BuildBoardData(ctx, slug)
	if err != nil {
		if errors.Is(err, services.ErrRankingNotFound) {
			a.notFound(w, r)
		} else {
			a.serverError(w, r, err)
		}
		return
	}

	base := a.base(r)
	view := a.assembleBuilderView(base, boardData)
	a.renderBoard(w, r, view)
}

// handleUpdateRanking is POST /r/{slug} — update title or description.
func (a *App) handleUpdateRanking(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slug := ctx.Value(auth.SlugKey).(uuid.UUID)
	title := r.FormValue("title")
	desc := r.FormValue("description")

	updated, err := a.RankingSvc.UpdateRanking(ctx, services.UpdateRankingRequest{
		Slug:        slug,
		Title:       title,
		Description: desc,
	})
	if err != nil {
		rankError(a, w, r, err)
		return
	}

	base := a.base(r)
	view := BuilderView{BaseView: base, Ranking: updated}
	if updated.UserID == nil {
		view.IsDraft = true
	}
	if err := a.Render.Partial(w, http.StatusOK, "partials/ranking_meta.html", view); err != nil {
		a.serverError(w, r, err)
	}
}

// handleSave is POST /r/{slug}/save — claim an anonymous draft or confirm save.
func (a *App) handleSave(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slug := ctx.Value(auth.SlugKey).(uuid.UUID)
	userID := a.Sessions.UserID(ctx)

	result, err := a.RankingSvc.SaveDraft(ctx, services.SaveDraftRequest{
		Slug:   slug,
		UserID: userID,
	})
	if err != nil {
		rankError(a, w, r, err)
		return
	}

	if result.Redirect != "" {
		a.Sessions.Flash(ctx, result.Message)
		http.Redirect(w, r, result.Redirect, http.StatusSeeOther)
	}
}

// handleAddItem is POST /r/{slug}/items — add a new item to the ranking.
func (a *App) handleAddItem(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slug := ctx.Value(auth.SlugKey).(uuid.UUID)
	label := strings.TrimSpace(r.FormValue("label"))

	if label == "" {
		base := a.base(r)
		view := BuilderView{BaseView: base, Error: "Give the item a name."}
		a.Render.Partial(w, http.StatusUnprocessableEntity, "partials/board.html", view)
		return
	}

	imageURL := r.FormValue("image_url")
	_, err := a.RankingSvc.AddItem(ctx, services.AddItemRequest{
		Slug:     slug,
		Label:    label,
		ImageURL: imageURL,
	})
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	ranking, err := a.RankingSvc.GetRankingForSlug(ctx, slug)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	base := a.base(r)
	view := a.assembleBuilderView(base, services.BoardData{Ranking: ranking.Ranking})
	a.renderBoard(w, r, view)
}

// handleDeleteItem is DELETE /r/{slug}/items/{itemID} — remove an item.
func (a *App) handleDeleteItem(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slug := ctx.Value(auth.SlugKey).(uuid.UUID)
	itemID := r.PathValue("itemID")
	id, err := strconv.ParseInt(itemID, 10, 64)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	err = a.RankingSvc.DeleteItem(r.Context(), services.DeleteItemRequest{
		Slug:   slug,
		ItemID: id,
	})
	if err != nil {
		rankError(a, w, r, err)
		return
	}

	ranking, err := a.RankingSvc.GetRankingForSlug(r.Context(), slug)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	base := a.base(r)
	view := a.assembleBuilderView(base, services.BoardData{
		Ranking: ranking.Ranking,
	})
	a.renderBoard(w, r, view)
}

// handleAddTier is POST /r/{slug}/tiers — add a new tier.
func (a *App) handleAddTier(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slug := ctx.Value(auth.SlugKey).(uuid.UUID)

	_, err := a.RankingSvc.AddTier(ctx, services.AddTierRequest{
		Slug:  slug,
		Label: r.FormValue("label"),
		Color: r.FormValue("color"),
	})
	if err != nil {
		rankError(a, w, r, err)
		return
	}

	ranking, err := a.RankingSvc.GetRankingForSlug(ctx, slug)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	base := a.base(r)
	view := a.assembleBuilderView(base, services.BoardData{Ranking: ranking.Ranking})
	a.renderBoard(w, r, view)
}

// handleEdit is POST /r/{slug}/tiers/{tierID}/edit — enable editing a tier
func (a *App) handleEditTier(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slug := ctx.Value(auth.SlugKey).(uuid.UUID)
	tierID := r.PathValue("tierID")
	id, err := strconv.ParseInt(tierID, 10, 64)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	tier, _, err := a.RankingSvc.GetTier(ctx, services.GetTierRequest{
		Slug:   slug,
		TierID: id,
	})
	if err != nil {
		rankError(a, w, r, err)
		return
	}

	view := TierRowLabelView{Tier: tier, RankingSlug: slug}
	a.Render.Partial(w, http.StatusAccepted, "partials/tier_row_label_editable.html", view)
}

// handleUpdateTier is PUT /r/{slug}/tiers/{tierID} — rename, recolor, or toggle.
func (a *App) handleUpdateTier(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slug := ctx.Value(auth.SlugKey).(uuid.UUID)
	tierID := r.PathValue("tierID")
	id, err := strconv.ParseInt(tierID, 10, 64)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	allowMultiple := r.FormValue("allow_multiple") == "true"

	tier, err := a.RankingSvc.UpdateTier(ctx, services.UpdateTierRequest{
		Slug:          slug,
		TierID:        id,
		Label:         r.FormValue("label"),
		Color:         r.FormValue("color"),
		AllowMultiple: allowMultiple,
	})
	if err != nil {
		rankError(a, w, r, err)
		return
	}

	view := TierRowLabelView{Tier: tier, RankingSlug: slug}
	a.Render.Partial(w, http.StatusAccepted, "partials/tier_row_label.html", view)
}

// handleDeleteTier is DELETE /r/{slug}/tiers/{tierID} — remove a tier.
func (a *App) handleDeleteTier(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slug := ctx.Value(auth.SlugKey).(uuid.UUID)
	tierID := r.PathValue("tierID")
	id, err := strconv.ParseInt(tierID, 10, 64)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	err = a.RankingSvc.DeleteTier(ctx, services.DeleteTierRequest{
		Slug:   slug,
		TierID: id,
	})
	if err != nil {
		rankError(a, w, r, err)
		return
	}

	ranking, err := a.RankingSvc.GetRankingForSlug(ctx, slug)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	base := a.base(r)
	view := a.assembleBuilderView(base, services.BoardData{Ranking: ranking.Ranking})
	a.renderBoard(w, r, view)
}

// handleSetPlacements is PUT /r/{slug}/placements — reorder items via drag-and-drop.
func (a *App) handleSetPlacements(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slug := ctx.Value(auth.SlugKey).(uuid.UUID)
	tierIDStr := r.FormValue("tier_id")
	tierID, err := strconv.ParseInt(tierIDStr, 10, 64)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	var itemIDs []int64
	for _, raw := range r.Form["item_id"] {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err == nil {
			itemIDs = append(itemIDs, id)
		}
	}

	err = a.RankingSvc.SetPlacements(ctx, services.SetPlacementsRequest{
		Slug:    slug,
		TierID:  tierID,
		ItemIDs: itemIDs,
	})
	if err != nil {
		if errors.Is(err, services.ErrInvalidTierPlacement) {
			ranking, _ := a.RankingSvc.GetRankingForSlug(ctx, slug)
			base := a.base(r)
			view := BuilderView{BaseView: base, Error: "tier holds a single item"}
			if ranking.UserID == nil {
				view.IsDraft = true
			}
			a.Render.Partial(w, http.StatusUnprocessableEntity, "partials/board.html", view)
			return
		}
		rankError(a, w, r, err)
		return
	}

	ranking, err := a.RankingSvc.GetRankingForSlug(ctx, slug)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	base := a.base(r)
	view := a.assembleBuilderView(base, services.BoardData{Ranking: ranking.Ranking})
	a.renderBoard(w, r, view)
}

// commaSplit splits a comma-separated string into trimmed, non-empty parts.
func commaSplit(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
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

	a.Sessions.Flash(r.Context(), "Account created! Your draft has been attached.")
	http.Redirect(w, r, "/me", http.StatusSeeOther)
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

	a.Sessions.Flash(r.Context(), "Welcome back!")
	http.Redirect(w, r, "/me", http.StatusSeeOther)
}

func (a *App) renderLoginError(w http.ResponseWriter, r *http.Request, email, next, errMsg string) {
	base := a.base(r)
	view := AuthView{BaseView: base, Email: email, Next: next, Error: errMsg}
	a.Render.Page(w, http.StatusUnauthorized, "pages/login.html", view)
}

// handleLogout is POST /logout.
func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	_ = a.UserSvc.Logout(r.Context())
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleRankings is GET /rankings — show signed-in user's saved rankings.
func (a *App) handleRankings(w http.ResponseWriter, r *http.Request) {
	userID := a.Sessions.UserID(r.Context())
	if userID == 0 {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	base := a.base(r)
	rankings, err := a.Queries.ListRankingsByUser(r.Context(), &userID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	accountRankings := make([]AccountRanking, len(rankings))
	for i, r := range rankings {
		var formatted string
		if r.UpdatedAt.Valid {
			formatted = r.UpdatedAt.Time.Format("Jan 2, 2006")
		}
		accountRankings[i] = AccountRanking{Ranking: r, FormattedUpdated: formatted}
	}
	view := AccountView{BaseView: base, Rankings: accountRankings}
	a.Render.Page(w, http.StatusOK, "pages/me.html", view)
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

// assembleBuilderView builds a BuilderView from service BoardData.
func (a *App) assembleBuilderView(base BaseView, bd services.BoardData) BuilderView {
	byID := make(map[int64]db.RankedItem, len(bd.Items))
	for _, it := range bd.Items {
		byID[it.ID] = it
	}
	placed := make(map[int64]bool, len(bd.Placements))

	tierItems := make(map[int64][]db.RankedItem, len(bd.Tiers))
	for _, p := range bd.Placements {
		it, ok := byID[p.RankedItemID]
		if !ok {
			continue
		}
		placed[p.RankedItemID] = true
		tierItems[p.RankingTierID] = append(tierItems[p.RankingTierID], it)
	}

	view := BuilderView{
		BaseView:         base,
		Ranking:          bd.Ranking,
		IsDraft:          bd.IsDraft,
		FormattedUpdated: bd.FormattedUpdated,
	}
	for _, t := range bd.Tiers {
		view.Tiers = append(view.Tiers, TierView{Tier: t, Items: tierItems[t.ID]})
	}
	for _, it := range bd.Items {
		if !placed[it.ID] {
			view.Unassigned = append(view.Unassigned, it)
		}
	}
	return view
}
