package services

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/ghmeier/rankanything/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	ErrRankingNotFound = errors.New("Ranking not found.")
	ErrInvalidTierPlacement = errors.New("Item cannot be placed on this tier.")
	ErrNotPublishable       = errors.New("This version is not ready to publish.")
	ErrInvalidLink          = errors.New("A link must be an http or https URL.")
	ErrInvalidColor         = errors.New("Color must be a hex color like #ff5500.")
	ErrTooManyItems         = errors.New("A ranking can have at most 500 items.")
	ErrTooManyTiers         = errors.New("A ranking can have at most 50 tiers.")
	ErrDraftAlreadyExists = errors.New("This ranking already has a draft in progress.")
)

var validHexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

type txBeginner interface {
	Begin(context.Context) (pgx.Tx, error)
}

type RankingsService struct {
	Queries *db.Queries
	Pool    txBeginner
}

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

func (s *RankingsService) GetRanking(ctx context.Context, id uuid.UUID) (db.Ranking, error) {
	ranking, err := s.Queries.GetRankingByUUID(ctx, id)
	if err != nil {
		return db.Ranking{}, ErrRankingNotFound
	}
	return ranking, nil
}

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
	// Nil means "not sent"; empty string means "clear this".
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
	SourceURL string
}

func (s *RankingsService) AddItem(ctx context.Context, req AddItemRequest) (db.RankingItem, error) {
	items, err := s.Queries.ListRankingItemsForVersion(ctx, req.VersionID)
	if err != nil {
		return db.RankingItem{}, err
	}
	if len(items) >= 500 {
		return db.RankingItem{}, ErrTooManyItems
	}

	imageURL, err := normalizeExternalURL(req.ImageSourceURL)
	if err != nil {
		return db.RankingItem{}, err
	}
	sourceURL, err := normalizeExternalURL(req.SourceURL)
	if err != nil {
		return db.RankingItem{}, err
	}

	var title *string
	if req.Title != "" {
		title = &req.Title
	}

	return s.Queries.CreateRankingItem(ctx, db.CreateRankingItemParams{
		RankingVersionID: req.VersionID,
		Title:            title,
		ImageSourceUrl:   imageURL,
		SourceUrl:        sourceURL,
	})
}

type GetItemRequest struct {
	VersionID int64
	ItemID    int64
}

func (s *RankingsService) GetItem(ctx context.Context, req GetItemRequest) (db.RankingItem, error) {
	return itemForVersion(ctx, s.Queries, req.ItemID, req.VersionID, ErrRankingNotFound)
}

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

	var title *string
	if req.Title != "" {
		title = &req.Title
	}

	return s.Queries.UpdateRankingItem(ctx, db.UpdateRankingItemParams{
		ID:             item.ID,
		Title:          title,
		ImageSourceUrl: imageURL,
		ImageUploadUrl: item.ImageUploadUrl,
		SourceUrl:      sourceURL,
	})
}

// Rejects non-http(s) schemes so javascript: URLs never reach the database.
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
	if !validHexColor.MatchString(color) {
		return db.RankingTier{}, ErrInvalidColor
	}

	tiers, err := s.Queries.ListRankingTiersForVersion(ctx, req.VersionID)
	if err != nil {
		return db.RankingTier{}, err
	}
	if len(tiers) >= 50 {
		return db.RankingTier{}, ErrTooManyTiers
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
	if !validHexColor.MatchString(color) {
		return db.RankingTier{}, ErrInvalidColor
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

func (s *RankingsService) DeleteTier(ctx context.Context, req DeleteTierRequest) error {
	tier, err := tierForVersion(ctx, s.Queries, req.TierID, req.VersionID, ErrRankingNotFound)
	if err != nil {
		return err
	}
	return s.Queries.DeleteRankingTier(ctx, tier.ID)
}

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

func itemForVersion(ctx context.Context, q *db.Queries, itemID, versionID int64, notFound error) (db.RankingItem, error) {
	item, err := q.GetRankingItem(ctx, itemID)
	if err != nil || item.RankingVersionID != versionID {
		return db.RankingItem{}, notFound
	}
	return item, nil
}

type ReorderTierItemsRequest struct {
	VersionID int64
	TierID    int64
	ItemIDs   []int64
}

// Transactional: the deferred unique index on position only holds at commit.
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

func (s *RankingsService) ListVersions(ctx context.Context, req ListVersionsRequest) ([]db.RankingVersion, error) {
	return s.Queries.ListRankingVersionsForRanking(ctx, req.RankingID)
}

type PublishValidation struct {
	Publishable bool
	Reasons     []string
}

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
			return db.RankingVersion{}, fmt.Errorf("copy item %d: %w", it.ID, err)
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

// 5 base32 bytes = 8 characters, matching the schema's length check.
func newShortUUID() (string, error) {
	b := make([]byte, 5)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate short uuid: %w", err)
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)), nil
}
