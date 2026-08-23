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

// DestroyAllForUser destroys every stored session belonging to userID. A
// password reset calls this so that a session an attacker already holds
// stops working the moment the real owner takes their account back —
// changing the password hash alone would leave that session live until it
// expired on its own.
//
// scs keeps no index from user to session, so Iterate walks every session in
// the store and this is a full scan of the sessions table. That is only
// affordable because password resets are rare; do not call it from a request
// path that runs often.
//
// This does not touch the caller's own in-flight session. That one is still
// held in the request context and LoadAndSave will write it back to the store
// after the handler returns, so the handler has to destroy it separately.
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
