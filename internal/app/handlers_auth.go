package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/ghmeier/rankanything/internal/auth"
	"github.com/ghmeier/rankanything/internal/render"
	"github.com/ghmeier/rankanything/internal/services"
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

	_, err = a.UserSvc.Register(r.Context(), services.RegisterRequest{
		Email:    email,
		Password: password,
		Next:     next,
	})
	if err != nil {
		if errors.Is(err, services.ErrEmailAlreadyRegistered) {
			a.renderRegisterError(w, r, email, next, "email already registered")
			return
		}
		a.serverError(w, r, err)
		return
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
