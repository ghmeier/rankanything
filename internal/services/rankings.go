// Package services holds the application's business logic — operations that
// manipulate domain state without knowledge of HTTP, templates, or the web.
package services

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"

	"github.com/ghmeier/rankanything/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	ErrRankingNotFound = errors.New("Ranking not found.")
	// ErrInvalidTierPlacement covers both a tier's normal capacity rules and
	// an attempt to place an item and a tier from different ranking versions
	// in the same operation — the latter is a cross-version placement, which
	// AddRankingItemToTier's explicit ranking_version_id column is designed
	// to make impossible.
	ErrInvalidTierPlacement = errors.New("Item cannot be placed on this tier.")
	// ErrNotPublishable is returned when publishing is attempted on a
	// version that fails the publish gate (see EvaluatePublishGate). The
	// UI hides the publish action in this case, so hitting this is either a
	// stale page or a direct request.
	ErrNotPublishable = errors.New("This version is not ready to publish.")
	// ErrDraftAlreadyExists guards ranking_versions_one_draft_idx: a ranking
	// can hold only one draft at a time, so branching a new one off a
	// published version fails while a draft is already in progress.
	ErrDraftAlreadyExists = errors.New("This ranking already has a draft in progress.")
)

// txBeginner is a database connection that can start transactions (savepoints).
type txBeginner interface {
	Begin(context.Context) (pgx.Tx, error)
}

// RankingsService owns all business logic for rankings, their versions,
// tiers, and items. It speaks in contexts, database models, and errors —
// never HTTP.
type RankingsService struct {
	Queries *db.Queries
	Pool    txBeginner
}

// RankingBoard holds everything needed to render one version of a ranking:
// its tiers, its items, and which items sit in which tier.
type RankingBoard struct {
	Ranking    db.Ranking
	Version    db.RankingVersion
	IsDraft    bool
	Tiers      []db.RankingTier
	Items      []db.RankingItem
	Placements []db.RankingItemTier
}

// CreateForUserRequest is the input for creating a user-owned ranking.
type CreateForUserRequest struct {
	UserID int64
}

// CreateForUser creates a ranking owned by the given user, along with its
// first draft version, and seeds that version with the default tier
// palette. There is no anonymous path: every ranking is owned from the
// moment it exists.
func (s *RankingsService) CreateForUser(ctx context.Context, req CreateForUserRequest) (db.Ranking, error) {
	ranking, err := s.Queries.CreateRanking(ctx, db.CreateRankingParams{
		Name:   "Untitled ranking",
		UserID: req.UserID,
	})
	if err != nil {
		return db.Ranking{}, fmt.Errorf("create ranking: %w", err)
	}

	short, err := newShortUUID()
	if err != nil {
		return db.Ranking{}, err
	}
	version, err := s.Queries.CreateRankingVersion(ctx, db.CreateRankingVersionParams{
		ShortUuid: short,
		RankingID: ranking.ID,
	})
	if err != nil {
		return db.Ranking{}, fmt.Errorf("create draft version: %w", err)
	}

	if err := seedDefaultTiers(ctx, s.Queries, ranking.ID, version.ID); err != nil {
		return db.Ranking{}, fmt.Errorf("seed tiers: %w", err)
	}

	return ranking, nil
}

// GetRanking fetches a ranking by its external uuid. It does not check
// ownership — that is RequireRankingAccess's job; this is the lookup it (and
// handlers that already know access is granted) build on.
func (s *RankingsService) GetRanking(ctx context.Context, id uuid.UUID) (db.Ranking, error) {
	ranking, err := s.Queries.GetRankingByUUID(ctx, id)
	if err != nil {
		return db.Ranking{}, ErrRankingNotFound
	}
	return ranking, nil
}

// ResolveVersion resolves which version of ranking a request addresses: the
// live version (most recently published, falling back to the draft) when
// shortUUID is empty, or the version pinned by that short_uuid otherwise.
func (s *RankingsService) ResolveVersion(ctx context.Context, ranking db.Ranking, shortUUID string) (db.RankingVersion, error) {
	if shortUUID == "" {
		return s.Queries.ResolveLiveRankingVersion(ctx, ranking.ID)
	}
	return s.Queries.GetRankingVersionByShortUUID(ctx, db.GetRankingVersionByShortUUIDParams{
		RankingID: ranking.ID,
		ShortUuid: shortUUID,
	})
}

// UpdateRankingRequest is the input for modifying a ranking's metadata.
type UpdateRankingRequest struct {
	UUID        uuid.UUID
	Name        string
	Description string
}

// UpdateRanking changes the title and/or description of a ranking.
func (s *RankingsService) UpdateRanking(ctx context.Context, req UpdateRankingRequest) (db.Ranking, error) {
	ranking, err := s.GetRanking(ctx, req.UUID)
	if err != nil {
		return db.Ranking{}, err
	}

	return s.Queries.UpdateRanking(ctx, db.UpdateRankingParams{
		ID:          ranking.ID,
		Name:        req.Name,
		Description: req.Description,
	})
}

// AddItemRequest is the input for adding an item to a ranking version.
type AddItemRequest struct {
	VersionID      int64
	Title          string
	ImageSourceURL string
}

// AddItem adds a new item to the given ranking version, unranked until it's
// placed in a tier.
func (s *RankingsService) AddItem(ctx context.Context, req AddItemRequest) (db.RankingItem, error) {
	var imageURL *string
	if req.ImageSourceURL != "" {
		imageURL = &req.ImageSourceURL
	}

	return s.Queries.CreateRankingItem(ctx, db.CreateRankingItemParams{
		RankingVersionID: req.VersionID,
		Title:            req.Title,
		ImageSourceUrl:   imageURL,
	})
}

// DeleteItemRequest is the input for removing an item.
type DeleteItemRequest struct {
	VersionID int64
	ItemID    int64
}

// DeleteItem removes an item and any tier placements it holds. It refuses to
// delete an item that does not belong to VersionID, so an item id from a
// ranking the caller does not have this version's access to can't be
// deleted by guessing an id.
func (s *RankingsService) DeleteItem(ctx context.Context, req DeleteItemRequest) error {
	item, err := s.Queries.GetRankingItem(ctx, req.ItemID)
	if err != nil {
		return ErrRankingNotFound
	}
	if item.RankingVersionID != req.VersionID {
		return ErrRankingNotFound
	}

	if err := s.Queries.RemoveRankingItemFromAllTiers(ctx, item.ID); err != nil {
		return err
	}
	return s.Queries.DeleteRankingItem(ctx, item.ID)
}

// AddTierRequest is the input for creating a new tier.
type AddTierRequest struct {
	VersionID int64
	RankingID int64
	Title     string
	Color     string
}

// AddTier creates a new tier at the end of the given ranking version.
func (s *RankingsService) AddTier(ctx context.Context, req AddTierRequest) (db.RankingTier, error) {
	title := req.Title
	if title == "" {
		title = "New tier"
	}
	color := req.Color
	if color == "" {
		color = "#94a3b8"
	}

	pos, err := s.Queries.NextRankingTierPosition(ctx, req.VersionID)
	if err != nil {
		return db.RankingTier{}, err
	}

	return s.Queries.CreateRankingTier(ctx, db.CreateRankingTierParams{
		RankingVersionID: req.VersionID,
		RankingID:        req.RankingID,
		Title:            title,
		ColorHex:         color,
		Position:         pos,
	})
}

// UpdateTierRequest is the input for modifying a tier's label or color.
type UpdateTierRequest struct {
	VersionID int64
	TierID    int64
	Title     string // empty keeps the existing title
	Color     string // empty keeps the existing color
}

// UpdateTier changes the title and/or color of a tier. It refuses to update
// a tier that does not belong to VersionID.
func (s *RankingsService) UpdateTier(ctx context.Context, req UpdateTierRequest) (db.RankingTier, error) {
	tier, err := s.getTierForVersion(ctx, req.TierID, req.VersionID)
	if err != nil {
		return db.RankingTier{}, err
	}

	title := req.Title
	if title == "" {
		title = tier.Title
	}
	color := req.Color
	if color == "" {
		color = tier.ColorHex
	}

	return s.Queries.UpdateRankingTier(ctx, db.UpdateRankingTierParams{
		ID:       tier.ID,
		Title:    title,
		ColorHex: color,
		Position: tier.Position,
	})
}

// GetTierRequest is the input for fetching a single tier.
type GetTierRequest struct {
	VersionID int64
	TierID    int64
}

// GetTier fetches a tier, refusing one that does not belong to VersionID.
func (s *RankingsService) GetTier(ctx context.Context, req GetTierRequest) (db.RankingTier, error) {
	return s.getTierForVersion(ctx, req.TierID, req.VersionID)
}

// DeleteTierRequest is the input for removing a tier.
type DeleteTierRequest struct {
	VersionID int64
	TierID    int64
}

// DeleteTier removes a tier from the ranking version. Its item placements
// cascade-delete with it — the items themselves survive, unranked, so a
// deleted tier returns its items to the unassigned tray rather than losing
// them.
func (s *RankingsService) DeleteTier(ctx context.Context, req DeleteTierRequest) error {
	tier, err := s.getTierForVersion(ctx, req.TierID, req.VersionID)
	if err != nil {
		return err
	}
	return s.Queries.DeleteRankingTier(ctx, tier.ID)
}

// getTierForVersion fetches a tier and confirms it belongs to versionID,
// translating both "no such tier" and "wrong version" to ErrRankingNotFound
// so a caller can't distinguish a bad id from someone else's tier.
func (s *RankingsService) getTierForVersion(ctx context.Context, tierID, versionID int64) (db.RankingTier, error) {
	tier, err := s.Queries.GetRankingTier(ctx, tierID)
	if err != nil {
		return db.RankingTier{}, ErrRankingNotFound
	}
	if tier.RankingVersionID != versionID {
		return db.RankingTier{}, ErrRankingNotFound
	}
	return tier, nil
}

// AddItemToTierRequest is the input for placing an item in a tier.
type AddItemToTierRequest struct {
	VersionID int64
	TierID    int64
	ItemID    int64
}

// AddItemToTier moves an item into a tier, clearing any prior placement it
// held first (the MVP places an item in one tier at a time, even though the
// schema allows more down the line). Both the tier and the item must belong
// to VersionID — the caller supplies it explicitly rather than the query
// inferring it, which is what makes placing an item against a tier from a
// different ranking version impossible.
func (s *RankingsService) AddItemToTier(ctx context.Context, req AddItemToTierRequest) (db.RankingItem, error) {
	tier, err := s.Queries.GetRankingTier(ctx, req.TierID)
	if err != nil {
		return db.RankingItem{}, ErrInvalidTierPlacement
	}
	item, err := s.Queries.GetRankingItem(ctx, req.ItemID)
	if err != nil {
		return db.RankingItem{}, ErrInvalidTierPlacement
	}
	if tier.RankingVersionID != req.VersionID || item.RankingVersionID != req.VersionID {
		return db.RankingItem{}, ErrInvalidTierPlacement
	}

	if err := s.Queries.RemoveRankingItemFromAllTiers(ctx, item.ID); err != nil {
		return db.RankingItem{}, err
	}

	pos, err := s.Queries.NextRankingItemTierPosition(ctx, tier.ID)
	if err != nil {
		return db.RankingItem{}, err
	}

	if _, err := s.Queries.AddRankingItemToTier(ctx, db.AddRankingItemToTierParams{
		RankingItemID:    item.ID,
		RankingTierID:    tier.ID,
		RankingVersionID: req.VersionID,
		Position:         pos,
	}); err != nil {
		return db.RankingItem{}, err
	}

	return item, nil
}

// itemForVersion fetches an item and confirms it belongs to versionID,
// translating both "no such item" and "wrong version" into
// ErrInvalidTierPlacement — the same treatment getTierForVersion gives
// tiers, so a caller can't distinguish a bad id from someone else's item.
func itemForVersion(ctx context.Context, q *db.Queries, itemID, versionID int64) (db.RankingItem, error) {
	item, err := q.GetRankingItem(ctx, itemID)
	if err != nil {
		return db.RankingItem{}, ErrInvalidTierPlacement
	}
	if item.RankingVersionID != versionID {
		return db.RankingItem{}, ErrInvalidTierPlacement
	}
	return item, nil
}

// ReorderTierItemsRequest is the input for setting a tier's full item order
// after a drag — the caller supplies every item that should end up in the
// tier, in order. An id already placed there is repositioned; any other id
// is inserted at that position, clearing whatever placement (or lack of
// one) it held before. This covers reordering within a tier and dragging an
// item in from another tier or the unranked tray in a single call.
type ReorderTierItemsRequest struct {
	VersionID int64
	TierID    int64
	ItemIDs   []int64
}

// ReorderTierItems sets a tier's item order transactionally: the deferred
// unique index on (ranking_tier_id, position) lets every row move through
// intermediate, possibly-colliding positions during the loop below, as long
// as the final positions (0..n-1) are unique when the transaction commits.
func (s *RankingsService) ReorderTierItems(ctx context.Context, req ReorderTierItemsRequest) ([]db.RankingItem, error) {
	tier, err := s.getTierForVersion(ctx, req.TierID, req.VersionID)
	if err != nil {
		return nil, err
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)

	existing, err := q.ListRankingItemTiersForTier(ctx, tier.ID)
	if err != nil {
		return nil, err
	}
	placementByItem := make(map[int64]db.RankingItemTier, len(existing))
	for _, p := range existing {
		placementByItem[p.RankingItemID] = p
	}

	items := make([]db.RankingItem, 0, len(req.ItemIDs))
	for i, itemID := range req.ItemIDs {
		item, err := itemForVersion(ctx, q, itemID, req.VersionID)
		if err != nil {
			return nil, err
		}

		if placement, ok := placementByItem[itemID]; ok {
			if _, err := q.ReorderRankingItemTier(ctx, db.ReorderRankingItemTierParams{
				ID:       placement.ID,
				Position: int16(i),
			}); err != nil {
				return nil, err
			}
		} else {
			if err := q.RemoveRankingItemFromAllTiers(ctx, item.ID); err != nil {
				return nil, err
			}
			if _, err := q.AddRankingItemToTier(ctx, db.AddRankingItemToTierParams{
				RankingItemID:    item.ID,
				RankingTierID:    tier.ID,
				RankingVersionID: req.VersionID,
				Position:         int16(i),
			}); err != nil {
				return nil, err
			}
		}
		items = append(items, item)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return items, nil
}

// ReorderTiersRequest is the input for setting a version's tier order after
// a drag — every tier id, in its new order.
type ReorderTiersRequest struct {
	VersionID int64
	TierIDs   []int64
}

// ReorderTiers sets every tier's position to match TierIDs, transactionally
// for the same reason ReorderTierItems is: the deferred unique index on
// (ranking_version_id, position) allows the loop's intermediate states to
// collide as long as the final set is unique.
func (s *RankingsService) ReorderTiers(ctx context.Context, req ReorderTiersRequest) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)

	for i, id := range req.TierIDs {
		tier, err := q.GetRankingTier(ctx, id)
		if err != nil {
			return ErrRankingNotFound
		}
		if tier.RankingVersionID != req.VersionID {
			return ErrRankingNotFound
		}
		if _, err := q.UpdateRankingTier(ctx, db.UpdateRankingTierParams{
			ID:       tier.ID,
			Title:    tier.Title,
			ColorHex: tier.ColorHex,
			Position: int16(i),
		}); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// UnrankItemRequest is the input for returning a single item to the
// unranked tray — used when drag-and-drop moves an item out of every tier.
type UnrankItemRequest struct {
	VersionID int64
	ItemID    int64
}

// UnrankItem clears an item's tier placement, if it has one.
func (s *RankingsService) UnrankItem(ctx context.Context, req UnrankItemRequest) (db.RankingItem, error) {
	item, err := itemForVersion(ctx, s.Queries, req.ItemID, req.VersionID)
	if err != nil {
		return db.RankingItem{}, err
	}
	if err := s.Queries.RemoveRankingItemFromAllTiers(ctx, item.ID); err != nil {
		return db.RankingItem{}, err
	}
	return item, nil
}

// ListUnrankedItems returns every item in a version with no tier placement —
// the unranked tray's contents.
func (s *RankingsService) ListUnrankedItems(ctx context.Context, versionID int64) ([]db.RankingItem, error) {
	items, err := s.Queries.ListRankingItemsForVersion(ctx, versionID)
	if err != nil {
		return nil, err
	}
	placements, err := s.Queries.ListRankingItemTiersForVersion(ctx, versionID)
	if err != nil {
		return nil, err
	}
	placed := make(map[int64]bool, len(placements))
	for _, p := range placements {
		placed[p.RankingItemID] = true
	}

	var unranked []db.RankingItem
	for _, it := range items {
		if !placed[it.ID] {
			unranked = append(unranked, it)
		}
	}
	return unranked, nil
}

// GetBoard fetches everything needed to render one version of a ranking.
func (s *RankingsService) GetBoard(ctx context.Context, ranking db.Ranking, version db.RankingVersion) (RankingBoard, error) {
	tiers, err := s.Queries.ListRankingTiersForVersion(ctx, version.ID)
	if err != nil {
		return RankingBoard{}, err
	}
	items, err := s.Queries.ListRankingItemsForVersion(ctx, version.ID)
	if err != nil {
		return RankingBoard{}, err
	}
	placements, err := s.Queries.ListRankingItemTiersForVersion(ctx, version.ID)
	if err != nil {
		return RankingBoard{}, err
	}

	return RankingBoard{
		Ranking:    ranking,
		Version:    version,
		IsDraft:    !version.PublishedAt.Valid,
		Tiers:      tiers,
		Items:      items,
		Placements: placements,
	}, nil
}

// ListVersionsRequest is the input for fetching every version of a ranking,
// for the board's version-picker dropdown.
type ListVersionsRequest struct {
	RankingID int64
}

// ListVersions returns every version of a ranking, most recently created
// first.
func (s *RankingsService) ListVersions(ctx context.Context, req ListVersionsRequest) ([]db.RankingVersion, error) {
	return s.Queries.ListRankingVersionsForRanking(ctx, req.RankingID)
}

// PublishGate reports whether a ranking version has enough structure to
// publish, and why not when it doesn't. Nothing in the schema expresses
// this — it's the MVP's schema decision 3: a version is publishable once it
// has at least one tier, at least one item, and every item placed in at
// least one tier (placement in more than one counts too; one is enough).
type PublishGate struct {
	Publishable bool
	Reasons     []string
}

// EvaluatePublishGate computes a version's PublishGate.
func (s *RankingsService) EvaluatePublishGate(ctx context.Context, versionID int64) (PublishGate, error) {
	tiers, err := s.Queries.ListRankingTiersForVersion(ctx, versionID)
	if err != nil {
		return PublishGate{}, err
	}
	items, err := s.Queries.ListRankingItemsForVersion(ctx, versionID)
	if err != nil {
		return PublishGate{}, err
	}

	var reasons []string
	if len(tiers) == 0 {
		reasons = append(reasons, "Add at least one tier.")
	}
	if len(items) == 0 {
		reasons = append(reasons, "Add at least one item.")
	} else {
		placements, err := s.Queries.ListRankingItemTiersForVersion(ctx, versionID)
		if err != nil {
			return PublishGate{}, err
		}
		placed := make(map[int64]bool, len(placements))
		for _, p := range placements {
			placed[p.RankingItemID] = true
		}
		var unplaced int
		for _, it := range items {
			if !placed[it.ID] {
				unplaced++
			}
		}
		if unplaced == 1 {
			reasons = append(reasons, "Place 1 more item into a tier.")
		} else if unplaced > 1 {
			reasons = append(reasons, fmt.Sprintf("Place %d more items into a tier.", unplaced))
		}
	}

	return PublishGate{Publishable: len(reasons) == 0, Reasons: reasons}, nil
}

// PublishVersionRequest is the input for publishing a draft.
type PublishVersionRequest struct {
	VersionID int64
}

// PublishVersion publishes a draft, refusing when it fails the publish
// gate.
func (s *RankingsService) PublishVersion(ctx context.Context, req PublishVersionRequest) (db.RankingVersion, error) {
	gate, err := s.EvaluatePublishGate(ctx, req.VersionID)
	if err != nil {
		return db.RankingVersion{}, err
	}
	if !gate.Publishable {
		return db.RankingVersion{}, ErrNotPublishable
	}
	return s.Queries.PublishRankingVersion(ctx, req.VersionID)
}

// CreateVersionFromPublishedRequest is the input for branching a new draft
// off a published version.
type CreateVersionFromPublishedRequest struct {
	RankingID       int64
	SourceVersionID int64
}

// CreateVersionFromPublished copies a published version's tiers, items, and
// tier placements into a fresh draft, so a ranking's owner can keep
// tweaking a ranking without touching the version that's currently live. A
// ranking holds only one draft at a time (ranking_versions_one_draft_idx),
// so this refuses when one already exists.
func (s *RankingsService) CreateVersionFromPublished(ctx context.Context, req CreateVersionFromPublishedRequest) (db.RankingVersion, error) {
	versions, err := s.Queries.ListRankingVersionsForRanking(ctx, req.RankingID)
	if err != nil {
		return db.RankingVersion{}, err
	}
	for _, v := range versions {
		if !v.PublishedAt.Valid {
			return db.RankingVersion{}, ErrDraftAlreadyExists
		}
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return db.RankingVersion{}, err
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)

	short, err := newShortUUID()
	if err != nil {
		return db.RankingVersion{}, err
	}
	version, err := q.CreateRankingVersion(ctx, db.CreateRankingVersionParams{ShortUuid: short, RankingID: req.RankingID})
	if err != nil {
		return db.RankingVersion{}, fmt.Errorf("create draft version: %w", err)
	}

	tiers, err := q.ListRankingTiersForVersion(ctx, req.SourceVersionID)
	if err != nil {
		return db.RankingVersion{}, err
	}
	tierIDs := make(map[int64]int64, len(tiers))
	for _, t := range tiers {
		newTier, err := q.CreateRankingTier(ctx, db.CreateRankingTierParams{
			RankingVersionID: version.ID,
			RankingID:        req.RankingID,
			Title:            t.Title,
			ColorHex:         t.ColorHex,
			Position:         t.Position,
		})
		if err != nil {
			return db.RankingVersion{}, fmt.Errorf("copy tier %s: %w", t.Title, err)
		}
		tierIDs[t.ID] = newTier.ID
	}

	items, err := q.ListRankingItemsForVersion(ctx, req.SourceVersionID)
	if err != nil {
		return db.RankingVersion{}, err
	}
	itemIDs := make(map[int64]int64, len(items))
	for _, it := range items {
		newItem, err := q.CreateRankingItem(ctx, db.CreateRankingItemParams{
			RankingVersionID: version.ID,
			Title:            it.Title,
			ImageSourceUrl:   it.ImageSourceUrl,
			SourceUrl:        it.SourceUrl,
		})
		if err != nil {
			return db.RankingVersion{}, fmt.Errorf("copy item %s: %w", it.Title, err)
		}
		itemIDs[it.ID] = newItem.ID
	}

	placements, err := q.ListRankingItemTiersForVersion(ctx, req.SourceVersionID)
	if err != nil {
		return db.RankingVersion{}, err
	}
	for _, p := range placements {
		newTierID, ok := tierIDs[p.RankingTierID]
		if !ok {
			continue
		}
		newItemID, ok := itemIDs[p.RankingItemID]
		if !ok {
			continue
		}
		if _, err := q.AddRankingItemToTier(ctx, db.AddRankingItemToTierParams{
			RankingItemID:    newItemID,
			RankingTierID:    newTierID,
			RankingVersionID: version.ID,
			Position:         p.Position,
		}); err != nil {
			return db.RankingVersion{}, fmt.Errorf("copy placement: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return db.RankingVersion{}, err
	}
	return version, nil
}

// DefaultTiers defines the S/A/B/C/D palette every new ranking version is
// seeded with.
var DefaultTiers = []struct {
	Label string
	Color string
}{
	{"S", "#f59e0b"},
	{"A", "#22c55e"},
	{"B", "#3b82f6"},
	{"C", "#a855f7"},
	{"D", "#64748b"},
}

// seedDefaultTiers creates the default tier palette for a freshly created
// ranking version.
func seedDefaultTiers(ctx context.Context, q *db.Queries, rankingID, versionID int64) error {
	for i, dt := range DefaultTiers {
		if _, err := q.CreateRankingTier(ctx, db.CreateRankingTierParams{
			RankingVersionID: versionID,
			RankingID:        rankingID,
			Title:            dt.Label,
			ColorHex:         dt.Color,
			Position:         int16(i),
		}); err != nil {
			return fmt.Errorf("seed tier %s: %w", dt.Label, err)
		}
	}
	return nil
}

// newShortUUID returns an 8-character lowercase identifier for addressing a
// version within its ranking (/r/{uuid}/v/{short}). 5 random bytes, base32
// encoded with no padding, is exactly 8 characters — matching the schema's
// short_uuid length check.
func newShortUUID() (string, error) {
	b := make([]byte, 5)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate short uuid: %w", err)
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)), nil
}
