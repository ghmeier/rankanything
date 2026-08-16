package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/ghmeier/rankanything/internal/auth"
	"github.com/ghmeier/rankanything/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ─── Builder routes ─────────────────────────────────────────────────────────

// handleHome is GET / — resume an existing draft or create a new one.
func (a *App) handleHome(w http.ResponseWriter, r *http.Request) {
	if userID := a.Sessions.UserID(r.Context()); userID != 0 {
		// Signed-in user: redirect to /new with a fresh ranking.
		http.Redirect(w, r, "/new", http.StatusSeeOther)
		return
	}

	// Anonymous user: try to resume an existing draft.
	if draftKeys := a.Sessions.Drafts(r.Context()); len(draftKeys) > 0 {
		if ranking, err := a.Queries.GetRankingBySlug(r.Context(), draftKeys[0]); err == nil {
			http.Redirect(w, r, "/r/"+ranking.Slug.String(), http.StatusSeeOther)
			return
		}

	}

	// No draft — create one.
	ranking, err := a.createDraft(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.Sessions.RememberDraft(r.Context(), ranking.Slug)
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

	ranking, err := a.Queries.CreateRanking(r.Context(), db.CreateRankingParams{
		Title:  "Untitled ranking",
		UserID: &userID,
	})
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := seedDefaultTiers(r.Context(), a.Queries, ranking.ID); err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/r/"+ranking.Slug.String(), http.StatusSeeOther)
}

// handleBuilder is GET /r/{slug} — render the board for a ranking.
func (a *App) handleBuilder(w http.ResponseWriter, r *http.Request) {
	ranking, ok := a.authorize(w, r)
	if !ok {
		return
	}
	base := a.base(r)
	view, err := a.buildView(r.Context(), base, ranking)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.renderBoard(w, r, view)
}

// handleUpdateRanking is POST /r/{slug} — update title or description.
func (a *App) handleUpdateRanking(w http.ResponseWriter, r *http.Request) {
	ranking, ok := a.authorize(w, r)
	if !ok {
		return
	}
	title := r.FormValue("title")
	desc := r.FormValue("description")
	if title == "" {
		title = ranking.Title
	}
	updated, err := a.Queries.UpdateRanking(r.Context(), db.UpdateRankingParams{
		ID:          ranking.ID,
		Title:       title,
		Description: desc,
	})
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	base := a.base(r)
	view := BuilderView{BaseView: base, Ranking: updated}
	if ranking.UserID == nil {
		view.IsDraft = true
	}
	if err := a.Render.Partial(w, http.StatusOK, "partials/ranking_meta.html", view); err != nil {
		a.serverError(w, r, err)
	}
}

// handleSave is POST /r/{slug}/save — claim an anonymous draft or confirm save.
func (a *App) handleSave(w http.ResponseWriter, r *http.Request) {
	ranking, ok := a.authorize(w, r)
	if !ok {
		return
	}

	if ranking.UserID != nil {
		// Already owned — just flash a message.
		a.Sessions.Flash(r.Context(), "Ranking saved!")
		http.Redirect(w, r, "/r/"+ranking.Slug.String(), http.StatusSeeOther)
		return
	}

	// Anonymous draft — redirect to login.
	next := "/r/" + ranking.Slug.String() + "/save"
	http.Redirect(w, r, "/login?next="+next, http.StatusSeeOther)
}

// handleAddItem is POST /r/{slug}/items — add a new item to the ranking.
func (a *App) handleAddItem(w http.ResponseWriter, r *http.Request) {
	ranking, ok := a.authorize(w, r)
	if !ok {
		return
	}

	label := r.FormValue("label")
	if label == "" {
		return
	}

	imageURL := r.FormValue("image_url")
	item, err := a.Queries.CreateItem(r.Context(), db.CreateItemParams{
		Label:    label,
		ImageUrl: imageURL,
	})
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := a.Queries.AddItemToRanking(r.Context(), db.AddItemToRankingParams{
		RankingID:    ranking.ID,
		RankedItemID: item.ID,
	}); err != nil {
		a.serverError(w, r, err)
		return
	}

	base := a.base(r)
	view, err := a.buildView(r.Context(), base, ranking)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.renderBoard(w, r, view)
}

// handleDeleteItem is DELETE /r/{slug}/items/{itemID} — remove an item.
func (a *App) handleDeleteItem(w http.ResponseWriter, r *http.Request) {
	ranking, ok := a.authorize(w, r)
	if !ok {
		return
	}

	itemID := r.PathValue("itemID")
	id, err := strconv.ParseInt(itemID, 10, 64)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	if err := a.Queries.RemoveItemFromTiers(r.Context(), db.RemoveItemFromTiersParams{
		RankingID:    ranking.ID,
		RankedItemID: id,
	}); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := a.Queries.RemoveItemFromRanking(r.Context(), db.RemoveItemFromRankingParams{
		RankingID:    ranking.ID,
		RankedItemID: id,
	}); err != nil {
		a.serverError(w, r, err)
		return
	}

	base := a.base(r)
	view, err := a.buildView(r.Context(), base, ranking)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.renderBoard(w, r, view)
}

// handleAddTier is POST /r/{slug}/tiers — add a new tier.
func (a *App) handleAddTier(w http.ResponseWriter, r *http.Request) {
	ranking, ok := a.authorize(w, r)
	if !ok {
		return
	}

	label := r.FormValue("label")
	color := r.FormValue("color")
	if label == "" {
		label = "New tier"
	}
	if color == "" {
		color = "#94a3b8"
	}

	pos, err := a.Queries.NextTierPosition(r.Context(), ranking.ID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	_, err = a.Queries.CreateTier(r.Context(), db.CreateTierParams{
		RankingID:     ranking.ID,
		Label:         label,
		Position:      pos,
		Color:         color,
		AllowMultiple: true,
	})
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	base := a.base(r)
	view, err := a.buildView(r.Context(), base, ranking)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.renderBoard(w, r, view)
}

// handleUpdateTier is PUT /r/{slug}/tiers/{tierID} — rename, recolor, or toggle.
func (a *App) handleUpdateTier(w http.ResponseWriter, r *http.Request) {
	ranking, ok := a.authorize(w, r)
	if !ok {
		return
	}

	tierID := r.PathValue("tierID")
	id, err := strconv.ParseInt(tierID, 10, 64)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	tier, err := a.Queries.GetTier(r.Context(), id)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	label := r.FormValue("label")
	if label == "" {
		label = tier.Label
	}
	color := r.FormValue("color")
	if color == "" {
		color = tier.Color
	}
	allowMultiple := r.FormValue("allow_multiple") == "true"

	_, err = a.Queries.UpdateTier(r.Context(), db.UpdateTierParams{
		ID:            tier.ID,
		Label:         label,
		Color:         color,
		Position:      tier.Position,
		AllowMultiple: allowMultiple,
	})
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	base := a.base(r)
	view, err := a.buildView(r.Context(), base, ranking)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.renderBoard(w, r, view)
}

// handleDeleteTier is DELETE /r/{slug}/tiers/{tierID} — remove a tier.
func (a *App) handleDeleteTier(w http.ResponseWriter, r *http.Request) {
	ranking, ok := a.authorize(w, r)
	if !ok {
		return
	}

	tierID := r.PathValue("tierID")
	id, err := strconv.ParseInt(tierID, 10, 64)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	if err := a.Queries.DeleteTier(r.Context(), id); err != nil {
		a.serverError(w, r, err)
		return
	}

	base := a.base(r)
	view, err := a.buildView(r.Context(), base, ranking)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	a.renderBoard(w, r, view)
}

// handleSetPlacements is PUT /r/{slug}/placements — reorder items via drag-and-drop.
// htmx-ext-sortable sends form values: tier_id=N & item_id=1&item_id=2&...
func (a *App) handleSetPlacements(w http.ResponseWriter, r *http.Request) {
	ranking, ok := a.authorize(w, r)
	if !ok {
		return
	}

	tierIDStr := r.FormValue("tier_id")
	tierID, err := strconv.ParseInt(tierIDStr, 10, 64)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	// Collect item_ids from the form (sortable sends them as repeated params).
	// Go's FormValue joins repeated params with commas.
	rawItemIDs := r.FormValue("item_id")
	var itemIDs []int64
	if rawItemIDs != "" {
		for _, part := range commaSplit(rawItemIDs) {
			id, err := strconv.ParseInt(part, 10, 64)
			if err == nil {
				itemIDs = append(itemIDs, id)
			}
		}
	}

	// tier_id == 0 means items were dropped into the unassigned tray.
	if tierID == 0 {
		for _, itemID := range itemIDs {
			if err := a.Queries.RemoveItemFromTiers(r.Context(), db.RemoveItemFromTiersParams{
				RankingID:    ranking.ID,
				RankedItemID: itemID,
			}); err != nil {
				a.serverError(w, r, err)
				return
			}
		}
	} else {
		// Check allow_multiple constraint before modifying.
		tier, err := a.Queries.GetTier(r.Context(), tierID)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		if !tier.AllowMultiple && len(itemIDs) > 1 {
			base := a.base(r)
			view, _ := a.buildView(r.Context(), base, ranking)
			view.Error = "This tier does not allow multiple items"
			a.renderBoard(w, r, view)
			return
		}

		// Clear existing placements for this tier.
		if err := a.Queries.ClearTierPlacements(r.Context(), tierID); err != nil {
			a.serverError(w, r, err)
			return
		}

		// Reinsert with new positions in a transaction to avoid constraint violations.
		tx, err := a.Pool.Begin(r.Context())
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		q := a.Queries.WithTx(tx)
		for i, itemID := range itemIDs {
			if err := q.InsertPlacement(r.Context(), db.InsertPlacementParams{
				RankingTierID: tierID,
				RankedItemID:  itemID,
				Position:      int32(i),
			}); err != nil {
				_ = tx.Rollback(r.Context())
				a.serverError(w, r, err)
				return
			}
		}
		if err := tx.Commit(r.Context()); err != nil {
			a.serverError(w, r, err)
			return
		}
	}

	base := a.base(r)
	view, err := a.buildView(r.Context(), base, ranking)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
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
	password := r.FormValue("password")
	hash, err := auth.HashPassword(password)
	if err != nil {
		a.renderRegisterError(w, r, email, next, "could not create account")
		return
	}

	user, err := a.Queries.CreateUser(r.Context(), db.CreateUserParams{
		Email:        email,
		PasswordHash: hash,
	})
	if err != nil {
		if isUniqueViolation(err) {
			a.renderRegisterError(w, r, email, next, "email already registered")
			return
		}
		a.serverError(w, r, err)
		return
	}

	if err := a.Sessions.LogIn(r.Context(), user.ID); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.Queries.TouchLastLogin(r.Context(), user.ID)

	// Claim any draft.
	draftKeys := a.Sessions.Drafts(r.Context())
	if len(draftKeys) > 0 {
		for _, slug := range draftKeys {
			_, cerr := a.Queries.ClaimRanking(r.Context(), db.ClaimRankingParams{
				Slug:   slug,
				UserID: &user.ID,
			})
			if cerr == nil {
				a.Sessions.ForgetDraft(r.Context(), slug)
			}

		}
	}

	a.Sessions.Flash(r.Context(), "Account created! Your draft has been attached.")
	target := next
	if target == "" {
		target = "/me"
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
	password := r.FormValue("password")

	user, err := a.Queries.GetUserByEmail(r.Context(), email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			a.renderLoginError(w, r, email, next, auth.ErrInvalidCredentials.Error())
			return
		}
		a.serverError(w, r, err)
		return
	}
	if !auth.CheckPassword(user.PasswordHash, password) {
		a.renderLoginError(w, r, email, next, auth.ErrInvalidCredentials.Error())
		return
	}

	if err := a.Sessions.LogIn(r.Context(), user.ID); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.Queries.TouchLastLogin(r.Context(), user.ID)

	// Claim any draft.
	draftKeys := a.Sessions.Drafts(r.Context())
	if len(draftKeys) > 0 {
		for _, slug := range draftKeys {
			_, cerr := a.Queries.ClaimRanking(r.Context(), db.ClaimRankingParams{
				Slug:   slug,
				UserID: &user.ID,
			})
			if cerr == nil {
				a.Sessions.ForgetDraft(r.Context(), slug)
			}
		}
	}

	a.Sessions.Flash(r.Context(), "Welcome back!")
	target := next
	if target == "" {
		target = "/me"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (a *App) renderLoginError(w http.ResponseWriter, r *http.Request, email, next, errMsg string) {
	base := a.base(r)
	view := AuthView{BaseView: base, Email: email, Next: next, Error: errMsg}
	a.Render.Page(w, http.StatusUnprocessableEntity, "pages/login.html", view)
}

// handleLogout is POST /logout.
func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	_ = a.Sessions.LogOut(r.Context())
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleMe is GET /me — show signed-in user's saved rankings.
func (a *App) handleMe(w http.ResponseWriter, r *http.Request) {
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

// isUniqueViolation reports whether an error is a PostgreSQL unique constraint violation.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// defaultTiers defines the S/A/B/C/D palette.
var DefaultTiers = []struct {
	Label         string
	Color         string
	AllowMultiple bool
}{
	{"S", "#f59e0b", false},
	{"A", "#22c55e", false},
	{"B", "#3b82f6", true},
	{"C", "#a855f7", true},
	{"D", "#64748b", true},
}

// seedDefaultTiers creates S/A/B/C/D tiers for a new ranking.
func seedDefaultTiers(ctx context.Context, q *db.Queries, rankingID int64) error {
	for i, dt := range DefaultTiers {
		pos := int32(i)
		if _, err := q.CreateTier(ctx, db.CreateTierParams{
			RankingID:     rankingID,
			Label:         dt.Label,
			Position:      pos,
			Color:         dt.Color,
			AllowMultiple: dt.AllowMultiple,
		}); err != nil {
			return fmt.Errorf("seed tier %s: %w", dt.Label, err)
		}
	}
	return nil
}

// createDraft creates a new anonymous ranking with default tiers.
func (a *App) createDraft(ctx context.Context) (db.Ranking, error) {
	ranking, err := a.Queries.CreateRanking(ctx, db.CreateRankingParams{
		Title:  "Untitled ranking",
		UserID: nil,
	})
	if err != nil {
		return db.Ranking{}, err
	}
	if err := seedDefaultTiers(ctx, a.Queries, ranking.ID); err != nil {
		return db.Ranking{}, fmt.Errorf("seed tiers: %w", err)
	}
	return ranking, nil
}
