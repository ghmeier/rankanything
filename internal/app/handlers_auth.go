package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/ghmeier/rankanything/internal/auth"
	"github.com/ghmeier/rankanything/internal/services"
	"github.com/ghmeier/rankanything/internal/ui"
)

func (a *App) handleRegisterForm(w http.ResponseWriter, r *http.Request) {
	props := a.registerProps(r)
	props.Next = r.URL.Query().Get("next")
	a.renderRegister(w, r, http.StatusOK, props)
}

func (a *App) registerProps(r *http.Request) ui.RegisterProps {
	base := a.base(r)
	return ui.RegisterProps{CSRFToken: base.CSRFToken, LoggedIn: base.User != nil, Flash: base.Flash}
}

func (a *App) renderRegister(w http.ResponseWriter, r *http.Request, status int, props ui.RegisterProps) {
	component := ui.RegisterPage(props)
	if isHTMXRequest(r) {
		component = ui.RegisterContainer(props)
	}
	a.render(w, r, status, component)
}

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

	if err := a.VerificationSvc.SendVerificationEmail(r.Context(), user.ID, user.Email); err != nil {
		a.Logger.Error("send verification email", "err", err, "user_id", user.ID)
	}

	a.Sessions.Flash(r.Context(), "Account created!")

	target := "/me"
	if next != "" && isSiteRelativePath(next) {
		target = next
	}

	redirect(w, r, target)
}

func (a *App) renderRegisterError(w http.ResponseWriter, r *http.Request, email, next, errMsg string) {
	props := a.registerProps(r)
	props.Email, props.Next, props.Error = email, next, errMsg
	a.renderRegister(w, r, http.StatusUnprocessableEntity, props)
}

func (a *App) renderRegisterConflict(w http.ResponseWriter, r *http.Request, email, next string) {
	props := a.registerProps(r)
	props.Email, props.Next, props.EmailAlreadyRegistered = email, next, true
	a.renderRegister(w, r, http.StatusUnprocessableEntity, props)
}

func (a *App) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	base := a.base(r)
	props := ui.LoginProps{
		CSRFToken: base.CSRFToken,
		LoggedIn:  base.User != nil,
		Flash:     base.Flash,
		Next:      r.URL.Query().Get("next"),
	}
	a.render(w, r, http.StatusOK, ui.LoginPage(props))
}

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

	redirect(w, r, target)
}

// Rejects protocol-relative and backslash-prefixed paths to prevent open redirects.
func isSiteRelativePath(next string) bool {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return false
	}
	return !strings.ContainsRune(next, '\\')
}

func (a *App) renderLoginError(w http.ResponseWriter, r *http.Request, email, next, errMsg string) {
	base := a.base(r)
	props := ui.LoginProps{
		CSRFToken: base.CSRFToken,
		LoggedIn:  base.User != nil,
		Flash:     base.Flash,
		Email:     email,
		Next:      next,
		Error:     errMsg,
	}
	component := ui.LoginPage(props)
	if isHTMXRequest(r) {
		component = ui.LoginContainer(props)
	}
	a.render(w, r, http.StatusUnauthorized, component)
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	_ = a.UserSvc.Logout(r.Context())
	redirect(w, r, "/")
}

func (a *App) handleVerifyEmail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tok := r.URL.Query().Get("token")

	if tok == "" || a.VerificationSvc.RedeemEmailVerification(ctx, tok) != nil {
		a.Sessions.Flash(ctx, "That verification link has expired or was already used. You can request a new one from the rankings page.")
	} else {
		a.Sessions.Flash(ctx, "Email verified!")
	}

	target := "/me"
	if a.Sessions.UserID(ctx) == 0 {
		target = "/login"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

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

	a.render(w, r, http.StatusOK, ui.ResendVerificationSentNotice(user.Email))
}

func (a *App) handleForgotPasswordForm(w http.ResponseWriter, r *http.Request) {
	base := a.base(r)
	props := ui.ForgotPasswordProps{CSRFToken: base.CSRFToken, LoggedIn: base.User != nil, Flash: base.Flash}
	a.render(w, r, http.StatusOK, ui.ForgotPasswordPage(props))
}

func (a *App) handleForgotPassword(w http.ResponseWriter, r *http.Request) {
	base := a.base(r)
	baseProps := ui.ForgotPasswordProps{CSRFToken: base.CSRFToken, LoggedIn: base.User != nil, Flash: base.Flash}

	emailInput, err := auth.NormalizeEmail(r.FormValue("email"))
	if err != nil {
		props := baseProps
		props.Email = r.FormValue("email")
		props.Error = err.Error()
		a.render(w, r, http.StatusUnprocessableEntity, ui.ForgotPasswordForm(props))
		return
	}

	if err := a.VerificationSvc.RequestPasswordReset(r.Context(), emailInput); err != nil {
		a.Logger.Error("request password reset", "err", err)
	}

	props := baseProps
	props.Sent = true
	a.render(w, r, http.StatusOK, ui.ForgotPasswordForm(props))
}

func (a *App) handleResetPasswordForm(w http.ResponseWriter, r *http.Request) {
	base := a.base(r)
	props := ui.ResetPasswordProps{
		CSRFToken: base.CSRFToken,
		LoggedIn:  base.User != nil,
		Flash:     base.Flash,
		Token:     r.URL.Query().Get("token"),
	}
	a.render(w, r, http.StatusOK, ui.ResetPasswordPage(props))
}

func (a *App) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	base := a.base(r)
	tok := r.FormValue("token")
	baseProps := ui.ResetPasswordProps{CSRFToken: base.CSRFToken, LoggedIn: base.User != nil, Flash: base.Flash, Token: tok}

	err := a.VerificationSvc.RedeemPasswordReset(r.Context(), tok, r.FormValue("password"))
	if err != nil {
		props := baseProps
		if errors.Is(err, auth.ErrWeakPassword) {
			props.Error = err.Error()
		} else {
			props.Error = "This link has expired or was already used. Request a new one."
		}
		a.render(w, r, http.StatusUnprocessableEntity, ui.ResetPasswordForm(props))
		return
	}

	if err := a.Sessions.LogOut(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}

	props := baseProps
	props.Done = true
	a.render(w, r, http.StatusOK, ui.ResetPasswordForm(props))
}
