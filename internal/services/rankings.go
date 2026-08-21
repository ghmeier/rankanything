// Package services holds the application's business logic — operations that
// manipulate domain state without knowledge of HTTP, templates, or the web.
package services

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/ghmeier/rankanything/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	ErrRankingNotFound      = errors.New("Ranking not found.")
	ErrInvalidTierPlacement = errors.New("Ranking cannot be placed on this tier.")
)

// txBeginner is a database connection that can start transactions (savepoints).
type txBeginner interface {
	Begin(context.Context) (pgx.Tx, error)
}

// RankingsService owns all business logic for rankings, tiers, and their items.
// It speaks in contexts, database models, and errors — never HTTP.
type RankingsService struct {
	Queries *db.Queries
	Pool    txBeginner
}

// Ranking bundles the ranking with metadata the handler needs.
type Ranking struct {
	db.Ranking
	IsDraft bool
}

// RankingWithItems holds the raw data needed to render a ranking's board.
type RankingWithItems struct {
	Ranking    db.Ranking
	IsDraft    bool
	Tiers      []db.RankingTier
	Items      []db.RankedItem
	Placements []db.RankingTierItem
}

type EnsureDraftRequest struct {
	DraftKeys []uuid.UUID
}

type GetRankingForSlugRequest struct {
	EnsureDraftRequest
	Slug uuid.UUID
}

// CreateDraft creates an anonymous ranking seeded with the default S/A/B/C/D
// tier palette.
func (s *RankingsService) EnsureDraft(ctx context.Context, req EnsureDraftRequest) (Ranking, error) {
	if len(req.DraftKeys) > 0 {
		ranking, err := s.Queries.GetRankingBySlug(ctx, req.DraftKeys[0])
		if err != nil {
			return Ranking{}, err
		}

		return Ranking{Ranking: ranking, IsDraft: true}, nil
	}

	ranking, err := s.Queries.CreateRanking(ctx, db.CreateRankingParams{
		Title:  "Untitled ranking",
		UserID: nil,
	})
	if err != nil {
		return Ranking{}, err
	}
	if err := seedDefaultTiers(ctx, s.Queries, ranking.ID); err != nil {
		return Ranking{}, fmt.Errorf("seed tiers: %w", err)
	}

	return Ranking{Ranking: ranking, IsDraft: true}, nil
}

// CreateForUserRequest is the input for creating a user-owned ranking.
type CreateForUserRequest struct {
	UserID int64
}

// CreateForUser creates a ranking owned by the given user and seeds it with
// the default tier palette.
func (s *RankingsService) CreateForUser(ctx context.Context, req CreateForUserRequest) (db.Ranking, error) {
	ranking, err := s.Queries.CreateRanking(ctx, db.CreateRankingParams{
		Title:  "Untitled ranking",
		UserID: &req.UserID,
	})
	if err != nil {
		return db.Ranking{}, err
	}
	if err := seedDefaultTiers(ctx, s.Queries, ranking.ID); err != nil {
		return db.Ranking{}, fmt.Errorf("seed tiers: %w", err)
	}
	return ranking, nil
}

// Authorize fetches a ranking by slug and checks session-based access.
// Anonymous users may only access drafts they created; signed-in users may
// only access rankings they own.  Returns ErrRankingNotFound when access is
// denied or the ranking doesn't exist.
func (s *RankingsService) GetRankingForSlug(ctx context.Context, slug uuid.UUID) (Ranking, error) {
	ranking, err := s.Queries.GetRankingBySlug(ctx, slug)
	if err != nil {
		return Ranking{}, ErrRankingNotFound
	}

	return Ranking{
		Ranking: ranking,
		IsDraft: ranking.UserID == nil,
	}, nil
}

// UpdateRankingRequest is the input for modifying a ranking's metadata.
type UpdateRankingRequest struct {
	Slug        uuid.UUID
	Title       string
	Description string
}

// UpdateRanking changes the title and/or description of a ranking.
func (s *RankingsService) UpdateRanking(ctx context.Context, req UpdateRankingRequest) (db.Ranking, error) {
	ranking, err := s.GetRankingForSlug(ctx, req.Slug)
	if err != nil {
		return db.Ranking{}, err
	}

	return s.Queries.UpdateRanking(ctx, db.UpdateRankingParams{
		ID:          ranking.ID,
		Title:       req.Title,
		Description: req.Description,
	})
}

// AddItemRequest is the input for adding an item to a ranking.
type AddItemRequest struct {
	Slug     uuid.UUID
	Label    string
	ImageURL string
}

// AddItem adds a new ranked item to the specified ranking.
func (s *RankingsService) AddItem(ctx context.Context, req AddItemRequest) (db.RankedItem, error) {
	ranking, err := s.GetRankingForSlug(ctx, req.Slug)
	if err != nil {
		return db.RankedItem{}, err
	}

	item, err := s.Queries.CreateItem(ctx, db.CreateItemParams{
		Label:    req.Label,
		ImageUrl: req.ImageURL,
	})
	if err != nil {
		return db.RankedItem{}, err
	}

	err = s.Queries.AddItemToRanking(ctx, db.AddItemToRankingParams{
		RankingID:    ranking.ID,
		RankedItemID: item.ID,
	})
	if err != nil {
		return db.RankedItem{}, err
	}

	return item, nil
}

// DeleteItemRequest is the input for removing an item from a ranking.
type DeleteItemRequest struct {
	Slug   uuid.UUID
	ItemID int64
}

// DeleteItem removes an item from a ranking and all its tier placements.
func (s *RankingsService) DeleteItem(ctx context.Context, req DeleteItemRequest) error {
	ranking, err := s.GetRankingForSlug(ctx, req.Slug)
	if err != nil {
		return err
	}

	if err := s.Queries.RemoveItemFromTiers(ctx, db.RemoveItemFromTiersParams{
		RankingID:    ranking.ID,
		RankedItemID: req.ItemID,
	}); err != nil {
		return err
	}

	return s.Queries.RemoveItemFromRanking(ctx, db.RemoveItemFromRankingParams{
		RankingID:    ranking.ID,
		RankedItemID: req.ItemID,
	})
}

// AddTierRequest is the input for creating a new tier.
type AddTierRequest struct {
	Slug  uuid.UUID
	Label string
	Color string
}

// AddTier creates a new tier at the end of the specified ranking.
func (s *RankingsService) AddTier(ctx context.Context, req AddTierRequest) (db.RankingTier, error) {
	ranking, err := s.GetRankingForSlug(ctx, req.Slug)
	if err != nil {
		return db.RankingTier{}, err
	}

	label := req.Label
	if label == "" {
		label = "New tier"
	}
	color := req.Color
	if color == "" {
		color = "#94a3b8"
	}

	pos, err := s.Queries.NextTierPosition(ctx, ranking.ID)
	if err != nil {
		return db.RankingTier{}, err
	}

	return s.Queries.CreateTier(ctx, db.CreateTierParams{
		RankingID:     ranking.ID,
		Label:         label,
		Position:      pos,
		Color:         color,
		AllowMultiple: true,
	})
}

// UpdateTierRequest is the input for modifying a tier's properties.
type UpdateTierRequest struct {
	Slug          uuid.UUID
	TierID        int64
	Label         string
	Color         string
	AllowMultiple *bool // nil means keep existing value
}

// UpdateTier changes the label, color, or multi-item setting of a tier.
func (s *RankingsService) UpdateTier(ctx context.Context, req UpdateTierRequest) (db.RankingTier, error) {
	tier, err := s.Queries.GetTier(ctx, req.TierID)
	if err != nil {
		return db.RankingTier{}, err
	}

	label := req.Label
	if label == "" {
		label = tier.Label
	}
	color := req.Color
	if color == "" {
		color = tier.Color
	}

	allowMultiple := tier.AllowMultiple
	if req.AllowMultiple != nil {
		allowMultiple = *req.AllowMultiple
	}

	tier, err = s.Queries.UpdateTier(ctx, db.UpdateTierParams{
		ID:            tier.ID,
		Label:         label,
		Color:         color,
		Position:      tier.Position,
		AllowMultiple: allowMultiple,
	})

	return tier, err
}

// GetTierRequest is the input for viewing a tier's editable contents.
type GetTierRequest struct {
	Slug   uuid.UUID
	TierID int64
}

// GetTier fetches a tier and its items for rendering an editable view.
func (s *RankingsService) GetTier(ctx context.Context, req GetTierRequest) (db.RankingTier, []db.RankedItem, error) {
	tier, err := s.Queries.GetTier(ctx, req.TierID)
	if err != nil {
		return db.RankingTier{}, nil, err
	}

	items, err := s.Queries.ListRankingTierItems(ctx, tier.ID)
	if err != nil {
		return db.RankingTier{}, nil, err
	}

	return tier, items, nil
}

// DeleteTierRequest is the input for removing a tier.
type DeleteTierRequest struct {
	Slug   uuid.UUID
	TierID int64
}

// DeleteTier removes a tier from the ranking.
func (s *RankingsService) DeleteTier(ctx context.Context, req DeleteTierRequest) error {
	return s.Queries.DeleteTier(ctx, req.TierID)
}

// AddItemToTierRequest is the input for reordering items via drag-and-drop.
type AddItemToTierRequest struct {
	Slug   uuid.UUID
	TierID int64
	ItemID int64
}

// AddItemToTier updates item positions, optionally clearing them into the
// unassigned tray when TierID is 0.
func (s *RankingsService) AddItemToTier(ctx context.Context, req AddItemToTierRequest) (db.RankedItem, error) {
	// Check allow_multiple constraint before modifying.
	tier, err := s.Queries.GetTier(ctx, req.TierID)
	if err != nil {
		return db.RankedItem{}, err
	}

	items, err := s.Queries.ListRankingTierItems(ctx, tier.ID)
	if err != nil {
		return db.RankedItem{}, err
	}

	itemIDs := make([]int64, len(items))
	for _, item := range items {
		itemIDs = append(itemIDs, item.ID)
	}

	if slices.Contains(itemIDs, req.ItemID) {
		return s.Queries.GetItem(ctx, req.ItemID)
	}

	if !tier.AllowMultiple && len(itemIDs) >= 1 {
		return db.RankedItem{}, ErrInvalidTierPlacement
	}

	err = s.Queries.AddItemToTier(ctx, db.AddItemToTierParams{RankingTierID: tier.ID, RankedItemID: req.ItemID, Position: int32(len(itemIDs))})
	if err != nil {
		return db.RankedItem{}, err
	}
	return s.Queries.GetItem(ctx, req.ItemID)
}

// SaveDraftRequest is the input for the save endpoint.
type SaveDraftRequest struct {
	Slug   uuid.UUID
	UserID int64 // 0 means the user is not signed in
}

// SaveDraftResult describes what the handler should do after save.
type SaveDraftResult struct {
	IsOwned  bool // true when the ranking is already user-owned
	Redirect string
	Message  string
}

// SaveDraft determines whether a draft can be saved or needs registration.
func (s *RankingsService) SaveDraft(ctx context.Context, req SaveDraftRequest) (SaveDraftResult, error) {
	ranking, err := s.GetRankingForSlug(ctx, req.Slug)
	if err != nil {
		return SaveDraftResult{}, err
	}

	if ranking.UserID != nil {
		return SaveDraftResult{
			IsOwned:  true,
			Redirect: "/r/" + ranking.Slug.String(),
			Message:  "Ranking saved!",
		}, nil
	}

	if req.UserID != 0 {
		return SaveDraftResult{
			IsOwned:  true,
			Redirect: "/r/" + ranking.Slug.String(),
			Message:  "Ranking saved!",
		}, nil
	}

	return SaveDraftResult{
		Redirect: "/register?next=/r/" + ranking.Slug.String(),
	}, nil
}

// GetRankingWithItems fetches all data needed to render the ranking board.
func (s *RankingsService) GetRankingWithItems(ctx context.Context, slug uuid.UUID) (RankingWithItems, error) {
	ranking, err := s.GetRankingForSlug(ctx, slug)
	if err != nil {
		return RankingWithItems{}, err
	}

	tiers, err := s.Queries.ListTiers(ctx, ranking.ID)
	if err != nil {
		return RankingWithItems{}, err
	}

	items, err := s.Queries.ListRankingItems(ctx, ranking.ID)
	if err != nil {
		return RankingWithItems{}, err
	}

	placements, err := s.Queries.ListRankingItemsByPosition(ctx, ranking.ID)
	if err != nil {
		return RankingWithItems{}, err
	}

	return RankingWithItems{
		Ranking:    ranking.Ranking,
		IsDraft:    ranking.IsDraft,
		Tiers:      tiers,
		Items:      items,
		Placements: placements,
	}, nil
}

// DefaultTiers defines the S/A/B/C/D palette.
var DefaultTiers = []struct {
	Label         string
	Color         string
	AllowMultiple bool
}{
	{"S", "#f59e0b", true},
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
