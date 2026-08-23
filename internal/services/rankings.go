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
