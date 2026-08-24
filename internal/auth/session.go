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

type Sessions struct {
	*scs.SessionManager
}

func NewSessions(m *scs.SessionManager) *Sessions { return &Sessions{SessionManager: m} }

// LogIn rotates the session token, defeating fixation.
func (s *Sessions) LogIn(ctx context.Context, userID int64) error {
	if err := s.RenewToken(ctx); err != nil {
		return err
	}
	s.Put(ctx, keyUserID, userID)
	return nil
}

func (s *Sessions) LogOut(ctx context.Context) error { return s.Destroy(ctx) }

// DestroyAllForUser drops every stored session for a user, so a reset takes
// the account back from anyone already holding one.
//
// scs keeps no user-to-session index, so this scans the whole sessions table.
// Only affordable because resets are rare; keep it off hot request paths.
//
// The caller's own in-flight session survives: LoadAndSave writes it back
// after the handler returns, so the handler must destroy it separately.
func (s *Sessions) DestroyAllForUser(ctx context.Context, userID int64) error {
	return s.Iterate(ctx, func(sessionCtx context.Context) error {
		if id, _ := s.Get(sessionCtx, keyUserID).(int64); id != userID {
			return nil
		}
		return s.Destroy(sessionCtx)
	})
}

// UserID returns the signed-in user's id, or 0.
func (s *Sessions) UserID(ctx context.Context) int64 {
	v, _ := s.Get(ctx, keyUserID).(int64)
	return v
}

// Flash stores a one-shot message rendered on the next page.
func (s *Sessions) Flash(ctx context.Context, msg string) { s.Put(ctx, keyFlash, msg) }

func (s *Sessions) PopFlash(ctx context.Context) string {
	return s.PopString(ctx, keyFlash)
}

// CSRFToken mints a token on first use.
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

func CookieDefaults(m *scs.SessionManager, secure bool) {
	m.Cookie.Name = "rankd_session"
	m.Cookie.HttpOnly = true
	m.Cookie.SameSite = http.SameSiteLaxMode
	m.Cookie.Secure = secure
	m.Cookie.Path = "/"
}
