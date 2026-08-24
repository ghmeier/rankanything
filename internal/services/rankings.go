// Package services holds the application's business logic — operations that
// manipulate domain state without knowledge of HTTP, templates, or the web.
package services

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/ghmeier/rankanything/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	ErrRankingNotFound = errors.New("Ranking not found.")
	// Also covers placing an item against a tier from another version.
	ErrInvalidTierPlacement = errors.New("Item cannot be placed on this tier.")
	ErrNotPublishable       = errors.New("This version is not ready to publish.")
	ErrInvalidLink          = errors.New("A link must be an http or https URL.")
	// Guards ranking_versions_one_draft_idx: one draft per ranking.
	ErrDraftAlreadyExists = errors.New("This ranking already has a draft in progress.")
)

type txBeginner interface {
	Begin(context.Context) (pgx.Tx, error)
}

type RankingsService struct {
	Queries *db.Queries
	Pool    txBeginner
}

// RankingBoard is everything needed to render one version of a ranking.
type RankingBoard struct {
	Ranking    db.Ranking
	Version    db.RankingVersion
	IsDraft    bool
	Tiers      []db.RankingTier
	Items      []db.RankingItem
	Placements []db.RankingItemTier
}

type CreateForUserRequest struct {
	UserID int64
}

// CreateForUser creates a ranking, its first draft version, and that
// version's default tiers.
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

// GetRanking does not check ownership; RequireRankingAccess does that.
func (s *RankingsService) GetRanking(ctx context.Context, id uuid.UUID) (db.Ranking, error) {
	ranking, err := s.Queries.GetRankingByUUID(ctx, id)
	if err != nil {
		return db.Ranking{}, ErrRankingNotFound
	}
	return ranking, nil
}

// ResolveVersion picks the live version when shortUUID is empty.
func (s *RankingsService) ResolveVersion(ctx context.Context, ranking db.Ranking, shortUUID string) (db.RankingVersion, error) {
	if shortUUID == "" {
		return s.Queries.ResolveLiveRankingVersion(ctx, ranking.ID)
	}
	return s.Queries.GetRankingVersionByShortUUID(ctx, db.GetRankingVersionByShortUUIDParams{
		RankingID: ranking.ID,
		ShortUuid: shortUUID,
	})
}

type UpdateRankingRequest struct {
	UUID uuid.UUID
	// Nil leaves the stored value alone. The title and the description are
	// edited by separate controls, and a request from one carries nothing
	// about the other — an empty string means "clear this", which is not the
	// same thing.
	Name        *string
	Description *string
}

func (s *RankingsService) UpdateRanking(ctx context.Context, req UpdateRankingRequest) (db.Ranking, error) {
	ranking, err := s.GetRanking(ctx, req.UUID)
	if err != nil {
		return db.Ranking{}, err
	}

	name := ranking.Name
	if req.Name != nil {
		name = *req.Name
	}
	description := ranking.Description
	if req.Description != nil {
		description = *req.Description
	}

	return s.Queries.UpdateRanking(ctx, db.UpdateRankingParams{
		ID:          ranking.ID,
		Name:        name,
		Description: description,
	})
}

type AddItemRequest struct {
	VersionID      int64
	Title          string
	ImageSourceURL string
	// SourceURL is where the item lives on the web — the restaurant's page,
	// the film's listing. Empty leaves the card unlinked.
	SourceURL string
}

func (s *RankingsService) AddItem(ctx context.Context, req AddItemRequest) (db.RankingItem, error) {
	imageURL, err := normalizeExternalURL(req.ImageSourceURL)
	if err != nil {
		return db.RankingItem{}, err
	}
	sourceURL, err := normalizeExternalURL(req.SourceURL)
	if err != nil {
		return db.RankingItem{}, err
	}

	return s.Queries.CreateRankingItem(ctx, db.CreateRankingItemParams{
		RankingVersionID: req.VersionID,
		Title:            req.Title,
		ImageSourceUrl:   imageURL,
		SourceUrl:        sourceURL,
	})
}

type GetItemRequest struct {
	VersionID int64
	ItemID    int64
}

// GetItem answers only for an item that belongs to the given version, so a
// handler cannot render one ranking's item inside another's board.
func (s *RankingsService) GetItem(ctx context.Context, req GetItemRequest) (db.RankingItem, error) {
	return itemForVersion(ctx, s.Queries, req.ItemID, req.VersionID, ErrRankingNotFound)
}

// UpdateItemRequest carries the item whole rather than a patch: it is filled
// from an edit form that shows every field at once.
type UpdateItemRequest struct {
	VersionID      int64
	ItemID         int64
	Title          string
	ImageSourceURL string
	SourceURL      string
}

func (s *RankingsService) UpdateItem(ctx context.Context, req UpdateItemRequest) (db.RankingItem, error) {
	item, err := itemForVersion(ctx, s.Queries, req.ItemID, req.VersionID, ErrRankingNotFound)
	if err != nil {
		return db.RankingItem{}, err
	}

	imageURL, err := normalizeExternalURL(req.ImageSourceURL)
	if err != nil {
		return db.RankingItem{}, err
	}
	sourceURL, err := normalizeExternalURL(req.SourceURL)
	if err != nil {
		return db.RankingItem{}, err
	}

	return s.Queries.UpdateRankingItem(ctx, db.UpdateRankingItemParams{
		ID:             item.ID,
		Title:          req.Title,
		ImageSourceUrl: imageURL,
		// Nothing writes an uploaded image yet, but the query sets every
		// column, so carry the stored value through rather than clearing it.
		ImageUploadUrl: item.ImageUploadUrl,
		SourceUrl:      sourceURL,
	})
}

// normalizeExternalURL keeps stored links to schemes a browser follows
// safely. templ.URL sanitizes again at render time, but a javascript: URL has
// no business reaching the database, and refusing it here is the difference
// between a rejected form and a link that quietly never works.
func normalizeExternalURL(raw string) (*string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}

	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, ErrInvalidLink
	}
	return &trimmed, nil
}

type DeleteItemRequest struct {
	VersionID int64
	ItemID    int64
}

// DeleteItem removes an item and any tier placements it holds.
func (s *RankingsService) DeleteItem(ctx context.Context, req DeleteItemRequest) error {
	item, err := itemForVersion(ctx, s.Queries, req.ItemID, req.VersionID, ErrRankingNotFound)
	if err != nil {
		return err
	}

	if err := s.Queries.RemoveRankingItemFromAllTiers(ctx, item.ID); err != nil {
		return err
	}
	return s.Queries.DeleteRankingItem(ctx, item.ID)
}

type AddTierRequest struct {
	VersionID int64
	RankingID int64
	Title     string
	Color     string
}

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

type UpdateTierRequest struct {
	VersionID int64
	TierID    int64
	Title     string // empty keeps the existing title
	Color     string // empty keeps the existing color
}

func (s *RankingsService) UpdateTier(ctx context.Context, req UpdateTierRequest) (db.RankingTier, error) {
	tier, err := tierForVersion(ctx, s.Queries, req.TierID, req.VersionID, ErrRankingNotFound)
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

type GetTierRequest struct {
	VersionID int64
	TierID    int64
}

func (s *RankingsService) GetTier(ctx context.Context, req GetTierRequest) (db.RankingTier, error) {
	return tierForVersion(ctx, s.Queries, req.TierID, req.VersionID, ErrRankingNotFound)
}

type DeleteTierRequest struct {
	VersionID int64
	TierID    int64
}

// DeleteTier cascade-deletes its placements, so its items survive unranked.
func (s *RankingsService) DeleteTier(ctx context.Context, req DeleteTierRequest) error {
	tier, err := tierForVersion(ctx, s.Queries, req.TierID, req.VersionID, ErrRankingNotFound)
	if err != nil {
		return err
	}
	return s.Queries.DeleteRankingTier(ctx, tier.ID)
}

// tierForVersion reports "no such tier" and "someone else's tier" the same
// way, so a caller can't tell a bad id from a tier it may not touch.
func tierForVersion(ctx context.Context, q *db.Queries, tierID, versionID int64, notFound error) (db.RankingTier, error) {
	tier, err := q.GetRankingTier(ctx, tierID)
	if err != nil || tier.RankingVersionID != versionID {
		return db.RankingTier{}, notFound
	}
	return tier, nil
}

type AddItemToTierRequest struct {
	VersionID int64
	TierID    int64
	ItemID    int64
}

// AddItemToTier clears any prior placement first: an item sits in one tier at
// a time, though the schema allows more.
func (s *RankingsService) AddItemToTier(ctx context.Context, req AddItemToTierRequest) (db.RankingItem, error) {
	tier, err := tierForVersion(ctx, s.Queries, req.TierID, req.VersionID, ErrInvalidTierPlacement)
	if err != nil {
		return db.RankingItem{}, err
	}
	item, err := itemForVersion(ctx, s.Queries, req.ItemID, req.VersionID, ErrInvalidTierPlacement)
	if err != nil {
		return db.RankingItem{}, err
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

// itemForVersion is tierForVersion's counterpart for items.
func itemForVersion(ctx context.Context, q *db.Queries, itemID, versionID int64, notFound error) (db.RankingItem, error) {
	item, err := q.GetRankingItem(ctx, itemID)
	if err != nil || item.RankingVersionID != versionID {
		return db.RankingItem{}, notFound
	}
	return item, nil
}

// ReorderTierItemsRequest carries every item that should end up in the tier,
// in order. An id not already placed there is moved in from wherever it was.
type ReorderTierItemsRequest struct {
	VersionID int64
	TierID    int64
	ItemIDs   []int64
}

// ReorderTierItems runs in one transaction because the deferred unique index
// on (ranking_tier_id, position) only has to hold at commit, letting rows
// pass through colliding positions on the way.
func (s *RankingsService) ReorderTierItems(ctx context.Context, req ReorderTierItemsRequest) ([]db.RankingItem, error) {
	tier, err := tierForVersion(ctx, s.Queries, req.TierID, req.VersionID, ErrRankingNotFound)
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
		item, err := itemForVersion(ctx, q, itemID, req.VersionID, ErrInvalidTierPlacement)
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

type ReorderTiersRequest struct {
	VersionID int64
	TierIDs   []int64
}

// ReorderTiers is transactional for the same reason ReorderTierItems is.
func (s *RankingsService) ReorderTiers(ctx context.Context, req ReorderTiersRequest) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := s.Queries.WithTx(tx)

	for i, id := range req.TierIDs {
		tier, err := tierForVersion(ctx, q, id, req.VersionID, ErrRankingNotFound)
		if err != nil {
			return err
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

type UnrankItemRequest struct {
	VersionID int64
	ItemID    int64
}

func (s *RankingsService) UnrankItem(ctx context.Context, req UnrankItemRequest) (db.RankingItem, error) {
	item, err := itemForVersion(ctx, s.Queries, req.ItemID, req.VersionID, ErrInvalidTierPlacement)
	if err != nil {
		return db.RankingItem{}, err
	}
	if err := s.Queries.RemoveRankingItemFromAllTiers(ctx, item.ID); err != nil {
		return db.RankingItem{}, err
	}
	return item, nil
}

// ListUnrankedItems returns the unranked tray's contents.
func (s *RankingsService) ListUnrankedItems(ctx context.Context, versionID int64) ([]db.RankingItem, error) {
	items, err := s.Queries.ListRankingItemsForVersion(ctx, versionID)
	if err != nil {
		return nil, err
	}
	placements, err := s.Queries.ListRankingItemTiersForVersion(ctx, versionID)
	if err != nil {
		return nil, err
	}
	return unplacedItems(items, placements), nil
}

// unplacedItems returns the items with no tier placement, in the order given.
func unplacedItems(items []db.RankingItem, placements []db.RankingItemTier) []db.RankingItem {
	placed := make(map[int64]bool, len(placements))
	for _, p := range placements {
		placed[p.RankingItemID] = true
	}

	var unplaced []db.RankingItem
	for _, it := range items {
		if !placed[it.ID] {
			unplaced = append(unplaced, it)
		}
	}
	return unplaced
}

// UnplacedItems returns this board's unranked tray contents.
func (b RankingBoard) UnplacedItems() []db.RankingItem {
	return unplacedItems(b.Items, b.Placements)
}

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

type ListVersionsRequest struct {
	RankingID int64
}

// ListVersions returns every version, most recently created first.
func (s *RankingsService) ListVersions(ctx context.Context, req ListVersionsRequest) ([]db.RankingVersion, error) {
	return s.Queries.ListRankingVersionsForRanking(ctx, req.RankingID)
}

// PublishValidation reports whether a version has enough structure to publish, and
// why not when it doesn't. Nothing in the schema expresses this rule.
type PublishValidation struct {
	Publishable bool
	Reasons     []string
}

// ValidatePublishable requires a tier, an item, and every item placed.
func (s *RankingsService) ValidatePublishable(ctx context.Context, versionID int64) (PublishValidation, error) {
	tiers, err := s.Queries.ListRankingTiersForVersion(ctx, versionID)
	if err != nil {
		return PublishValidation{}, err
	}
	items, err := s.Queries.ListRankingItemsForVersion(ctx, versionID)
	if err != nil {
		return PublishValidation{}, err
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
			return PublishValidation{}, err
		}
		unplaced := len(unplacedItems(items, placements))
		if unplaced == 1 {
			reasons = append(reasons, "Place 1 more item into a tier.")
		} else if unplaced > 1 {
			reasons = append(reasons, fmt.Sprintf("Place %d more items into a tier.", unplaced))
		}
	}

	return PublishValidation{Publishable: len(reasons) == 0, Reasons: reasons}, nil
}

type PublishVersionRequest struct {
	VersionID int64
}

func (s *RankingsService) PublishVersion(ctx context.Context, req PublishVersionRequest) (db.RankingVersion, error) {
	validation, err := s.ValidatePublishable(ctx, req.VersionID)
	if err != nil {
		return db.RankingVersion{}, err
	}
	if !validation.Publishable {
		return db.RankingVersion{}, ErrNotPublishable
	}
	return s.Queries.PublishRankingVersion(ctx, req.VersionID)
}

type CreateVersionFromPublishedRequest struct {
	RankingID       int64
	SourceVersionID int64
}

// CreateVersionFromPublished copies a published version into a fresh draft so
// the owner can keep editing without disturbing what is live.
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

func FormatVersionLabel(v db.RankingVersion, number int) string {
	if !v.PublishedAt.Valid {
		return "Draft"
	}
	return fmt.Sprintf("v%d · Published %s", number, FormatPublishedAt(v))
}

func FormatPublishedAt(v db.RankingVersion) string {
	if !v.PublishedAt.Valid {
		return ""
	}

	oneYearAgo := time.Now().AddDate(-1, 0, 0)
	if v.PublishedAt.Time.After(oneYearAgo) {
		return v.PublishedAt.Time.Format("Jan 2")
	}

	return v.PublishedAt.Time.Format("Jan 2, 2006")
}

// DefaultTiers seeds every new ranking version.
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

// newShortUUID addresses a version within its ranking. 5 base32 bytes is
// exactly the 8 characters the schema's length check wants.
func newShortUUID() (string, error) {
	b := make([]byte, 5)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate short uuid: %w", err)
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)), nil
}
