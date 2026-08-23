package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/ghmeier/rankanything/internal/auth"
	"github.com/ghmeier/rankanything/internal/render"
	"github.com/ghmeier/rankanything/internal/services"
	"github.com/ghmeier/rankanything/internal/ui"
)

// handleRegisterForm is GET /register.
func (a *App) handleRegisterForm(w http.ResponseWriter, r *http.Request) {
	base := a.base(r)
	next := r.URL.Query().Get("next")
	view := AuthView{BaseView: base, Next: next}
	a.Render.Page(w, http.StatusOK, "pages/register.html", view)
}

// handleRegister is POST /register.
func (a *App) handleRegister(w http.ResponseWriter, r *http.Request) {
	next := r.FormValue("next")
	email, err := auth.NormalizeEmail(r.FormValue("email"))
	if err != nil {
		a.renderRegisterError(w, r, email, next, err.Error())
		return
	}
	password, err := auth.HashPassword(r.FormValue("password"))
	if err != nil {
		a.renderRegisterError(w, r, email, next, err.Error())
		return
	}

	user, err := a.UserSvc.Register(r.Context(), services.RegisterRequest{
		Email:    email,
		Password: password,
		Next:     next,
	})
	if err != nil {
		if errors.Is(err, services.ErrEmailAlreadyRegistered) {
			a.renderRegisterConflict(w, r, email, next)
			return
		}
		a.serverError(w, r, err)
		return
	}

	// A failed send here shouldn't fail registration itself — the account
	// still exists, and the rankings index's resend control covers a user
	// whose first verification mail never arrived.
	if err := a.VerificationSvc.SendVerificationEmail(r.Context(), user.ID, user.Email); err != nil {
		a.Logger.Error("send verification email", "err", err, "user_id", user.ID)
	}

	a.Sessions.Flash(r.Context(), "Account created!")

	// next is untrusted user input; only follow it when it stays on this
	// site, the same guard handleLogin uses, so registration can't be used
	// as an open redirect.
	target := "/me"
	if next != "" && isSiteRelativePath(next) {
		target = next
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (a *App) renderRegisterError(w http.ResponseWriter, r *http.Request, email, next, errMsg string) {
	base := a.base(r)
	view := AuthView{BaseView: base, Email: email, Next: next, Error: errMsg}
	a.Render.Page(w, http.StatusUnprocessableEntity, "pages/register.html", view)
}

func (a *App) renderRegisterConflict(w http.ResponseWriter, r *http.Request, email, next string) {
	base := a.base(r)
	view := AuthView{BaseView: base, Email: email, Next: next, EmailAlreadyRegistered: true}
	a.Render.Page(w, http.StatusUnprocessableEntity, "pages/register.html", view)
}

// handleLoginForm is GET /login.
func (a *App) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	base := a.base(r)
	next := r.URL.Query().Get("next")
	view := AuthView{BaseView: base, Next: next}
	a.Render.Page(w, http.StatusOK, "pages/login.html", view)
}

// handleLogin is POST /login.
func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	next := r.FormValue("next")
	email, err := auth.NormalizeEmail(r.FormValue("email"))
	if err != nil {
		a.renderLoginError(w, r, email, next, err.Error())
		return
	}

	_, err = a.UserSvc.Login(r.Context(), services.LoginRequest{
		Email:    email,
		Password: r.FormValue("password"),
		Next:     next,
	})
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			a.renderLoginError(w, r, email, next, auth.ErrInvalidCredentials.Error())
			return
		}
		a.serverError(w, r, err)
		return
	}

	target := "/"
	if next != "" && isSiteRelativePath(next) {
		target = next
	}

	if render.IsHTMXRequest(r) {
		w.Header().Set("HX-Redirect", target)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// isSiteRelativePath reports whether next is safe to redirect to: a path on
// this site, not an absolute or protocol-relative URL that could send the
// user elsewhere (an open redirect). A leading "/\" is also rejected —
// some browsers normalize a backslash there into a second forward slash,
// turning what looks like a site-relative path into a protocol-relative one.
func isSiteRelativePath(next string) bool {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return false
	}
	return !strings.ContainsRune(next, '\\')
}

func (a *App) renderLoginError(w http.ResponseWriter, r *http.Request, email, next, errMsg string) {
	base := a.base(r)
	view := AuthView{BaseView: base, Email: email, Next: next, Error: errMsg}
	err := a.Render.Partial(w, http.StatusUnauthorized, "pages/login.html", view)
	if err != nil {
		a.serverError(w, r, err)
	}
}

// handleLogout is POST /logout.
func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	_ = a.UserSvc.Logout(r.Context())
	if render.IsHTMXRequest(r) {
		w.Header().Set("HX-Redirect", "/")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleVerifyEmail is GET /verify — the click-through from a verification
// email. It's a plain redirect rather than a page of its own: there's
// nothing for the visitor to do here but land somewhere with a flash
// explaining what happened.
func (a *App) handleVerifyEmail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tok := r.URL.Query().Get("token")

	if tok == "" || a.VerificationSvc.RedeemEmailVerification(ctx, tok) != nil {
		a.Sessions.Flash(ctx, "That verification link has expired or was already used. You can request a new one from your rankings page.")
	} else {
		a.Sessions.Flash(ctx, "Email verified!")
	}

	// A signed-out visitor (verifying from a different browser than the one
	// they registered in) would just bounce off RequireUser at /me; sending
	// them to /login instead means the flash is the first thing they see.
	target := "/me"
	if a.Sessions.UserID(ctx) == 0 {
		target = "/login"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// handleResendVerification is POST /resend-verification, gated by
// RequireUser — it always acts on the signed-in user's own address, so
// there's no neutral-response concern the way there is for forgot-password.
func (a *App) handleResendVerification(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, err := a.Queries.GetUserByID(ctx, a.Sessions.UserID(ctx))
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	if !user.EmailVerified {
		if err := a.VerificationSvc.SendVerificationEmail(ctx, user.ID, user.Email); err != nil {
			a.Logger.Error("resend verification email", "err", err, "user_id", user.ID)
		}
	}

	a.renderVerificationNotice(w, r, user.Email)
}

func (a *App) renderVerificationNotice(w http.ResponseWriter, r *http.Request, email string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := ui.ResendVerificationSentNotice(email).Render(r.Context(), w); err != nil {
		a.Logger.Error("render verification notice", "err", err)
	}
}

// handleForgotPasswordForm is GET /forgot-password.
func (a *App) handleForgotPasswordForm(w http.ResponseWriter, r *http.Request) {
	base := a.base(r)
	props := ui.ForgotPasswordProps{CSRFToken: base.CSRFToken, LoggedIn: base.User != nil, Flash: base.Flash}
	a.renderForgotPasswordPage(w, r, http.StatusOK, props)
}

// handleForgotPassword is POST /forgot-password. Its response is identical
// whether or not the address has an account — see
// services.VerificationService.RequestPasswordReset — so nothing here may
// branch on whether the account existed.
func (a *App) handleForgotPassword(w http.ResponseWriter, r *http.Request) {
	base := a.base(r)
	baseProps := ui.ForgotPasswordProps{CSRFToken: base.CSRFToken, LoggedIn: base.User != nil, Flash: base.Flash}

	emailInput, err := auth.NormalizeEmail(r.FormValue("email"))
	if err != nil {
		props := baseProps
		props.Email = r.FormValue("email")
		props.Error = err.Error()
		a.renderForgotPasswordForm(w, r, http.StatusUnprocessableEntity, props)
		return
	}

	if err := a.VerificationSvc.RequestPasswordReset(r.Context(), emailInput); err != nil {
		a.Logger.Error("request password reset", "err", err)
	}

	props := baseProps
	props.Sent = true
	a.renderForgotPasswordForm(w, r, http.StatusOK, props)
}

func (a *App) renderForgotPasswordPage(w http.ResponseWriter, r *http.Request, status int, props ui.ForgotPasswordProps) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := ui.ForgotPasswordPage(props).Render(r.Context(), w); err != nil {
		a.Logger.Error("render forgot-password page", "err", err)
	}
}

func (a *App) renderForgotPasswordForm(w http.ResponseWriter, r *http.Request, status int, props ui.ForgotPasswordProps) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := ui.ForgotPasswordForm(props).Render(r.Context(), w); err != nil {
		a.Logger.Error("render forgot-password form", "err", err)
	}
}

// handleResetPasswordForm is GET /reset-password?token=...
func (a *App) handleResetPasswordForm(w http.ResponseWriter, r *http.Request) {
	base := a.base(r)
	props := ui.ResetPasswordProps{
		CSRFToken: base.CSRFToken,
		LoggedIn:  base.User != nil,
		Flash:     base.Flash,
		Token:     r.URL.Query().Get("token"),
	}
	a.renderResetPasswordPage(w, r, http.StatusOK, props)
}

// handleResetPassword is POST /reset-password. Unlike forgot-password, a
// bad token here doesn't need a neutral response — reaching this endpoint
// already proves the caller holds a link that was mailed out, so the error
// can say plainly that it's expired or used rather than staying vague.
func (a *App) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	base := a.base(r)
	tok := r.FormValue("token")
	baseProps := ui.ResetPasswordProps{CSRFToken: base.CSRFToken, LoggedIn: base.User != nil, Flash: base.Flash, Token: tok}

	// The password goes to the service in plaintext rather than pre-hashed
	// here, because the service hashes only after the token checks out. See
	// services.VerificationService.RedeemPasswordReset.
	err := a.VerificationSvc.RedeemPasswordReset(r.Context(), tok, r.FormValue("password"))
	if err != nil {
		props := baseProps
		if errors.Is(err, auth.ErrWeakPassword) {
			props.Error = err.Error()
		} else {
			props.Error = "This link has expired or was already used. Request a new one."
		}
		a.renderResetPasswordForm(w, r, http.StatusUnprocessableEntity, props)
		return
	}

	// The service dropped this account's stored sessions, but the session this
	// request arrived on is still held in the context, and LoadAndSave would
	// write it back to the store after this handler returns. Destroying it
	// here ends a reset performed while signed in; when signed out there is
	// nothing to destroy and this is a no-op.
	if err := a.Sessions.LogOut(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}

	props := baseProps
	props.Done = true
	a.renderResetPasswordForm(w, r, http.StatusOK, props)
}

func (a *App) renderResetPasswordPage(w http.ResponseWriter, r *http.Request, status int, props ui.ResetPasswordProps) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := ui.ResetPasswordPage(props).Render(r.Context(), w); err != nil {
		a.Logger.Error("render reset-password page", "err", err)
	}
}

func (a *App) renderResetPasswordForm(w http.ResponseWriter, r *http.Request, status int, props ui.ResetPasswordProps) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := ui.ResetPasswordForm(props).Render(r.Context(), w); err != nil {
		a.Logger.Error("render reset-password form", "err", err)
	}
}
