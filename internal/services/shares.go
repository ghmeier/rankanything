package services

import "github.com/ghmeier/rankanything/internal/db"

// ShareService will own public-link sharing once feat/public-share (wave 4)
// lands: toggling ranking_shares.is_public, minting and clearing
// public_slug, and resolving a public_slug to its ranking's live version.
// Declared now, empty, so internal/app.App can hold a ShareSvc field without
// that branch needing to add one to a struct every sibling branch also
// touches.
type ShareService struct {
	Queries *db.Queries
}
