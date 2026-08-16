package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/gob"
	"net/http"
	"slices"

	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"
)

const (
	keyUserID = "user_id"
	keyDrafts = "draft_keys"
	keyCSRF   = "csrf_token"
	keyFlash  = "flash"
)

func init() {
	gob.Register([]uuid.UUID{})
}

// Sessions wraps the scs manager with the app's session vocabulary.
type Sessions struct {
	*scs.SessionManager
}

func NewSessions(m *scs.SessionManager) *Sessions { return &Sessions{SessionManager: m} }

// LogIn rotates the session token (defeating fixation) and records the user.
func (s *Sessions) LogIn(ctx context.Context, userID int64) error {
	if err := s.RenewToken(ctx); err != nil {
		return err
	}
	s.Put(ctx, keyUserID, userID)
	return nil
}

// LogOut destroys the session entirely.
func (s *Sessions) LogOut(ctx context.Context) error { return s.Destroy(ctx) }

// UserID returns the signed-in user's id, or 0.
func (s *Sessions) UserID(ctx context.Context) int64 {
	v, _ := s.Get(ctx, keyUserID).(int64)
	return v
}

// RememberDraft records that this session owns an unclaimed ranking.
func (s *Sessions) RememberDraft(ctx context.Context, slug uuid.UUID) {
	drafts := s.Drafts(ctx)
	if !slices.Contains(drafts, slug) {
		drafts = append(drafts, slug)
	}
	s.Put(ctx, keyDrafts, drafts)
}

// Drafts lists unclaimed ranking slugs this session created.
func (s *Sessions) Drafts(ctx context.Context) []uuid.UUID {
	v, _ := s.Get(ctx, keyDrafts).([]uuid.UUID)
	return v
}

// OwnsDraft reports whether this session created the given unclaimed ranking.
func (s *Sessions) OwnsDraft(ctx context.Context, slug uuid.UUID) bool {
	return slices.Contains(s.Drafts(ctx), slug)
}

// ForgetDraft drops a slug once it has been claimed.
func (s *Sessions) ForgetDraft(ctx context.Context, slug uuid.UUID) {
	drafts := s.Drafts(ctx)
	out := drafts[:0]
	for _, d := range drafts {
		if d != slug {
			out = append(out, d)
		}
	}
	s.Put(ctx, keyDrafts, slices.Clone(out))
}

// Flash stores a one-shot message rendered on the next page.
func (s *Sessions) Flash(ctx context.Context, msg string) { s.Put(ctx, keyFlash, msg) }

// PopFlash reads and clears the flash message.
func (s *Sessions) PopFlash(ctx context.Context) string {
	return s.PopString(ctx, keyFlash)
}

// CSRFToken returns the session's CSRF token, minting one on first use.
func (s *Sessions) CSRFToken(ctx context.Context) string {
	if tok, ok := s.Get(ctx, keyCSRF).(string); ok && tok != "" {
		return tok
	}
	tok := randomToken()
	s.Put(ctx, keyCSRF, tok)
	return tok
}

func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// CookieDefaults applies the app's cookie policy to an scs manager.
func CookieDefaults(m *scs.SessionManager, secure bool) {
	m.Cookie.Name = "rankd_session"
	m.Cookie.HttpOnly = true
	m.Cookie.SameSite = http.SameSiteLaxMode
	m.Cookie.Secure = secure
	m.Cookie.Path = "/"
}
