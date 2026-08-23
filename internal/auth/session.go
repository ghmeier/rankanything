package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"

	"github.com/alexedwards/scs/v2"
)

const (
	keyUserID = "user_id"
	keyCSRF   = "csrf_token"
	keyFlash  = "flash"
)

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
