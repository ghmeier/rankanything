package app

import (
	"net/http"

	"github.com/a-h/templ"
	"github.com/ghmeier/rankanything/internal/db"
)

// BaseView carries what the layout needs on every page.
type BaseView struct {
	User      *db.User
	CSRFToken string
	Flash     string
	// Theme is empty for a signed-out visitor — see LayoutProps.Theme for
	// what that does to the rendered <html> tag.
	Theme string
}

func (a *App) base(r *http.Request) BaseView {
	ctx := r.Context()
	v := BaseView{CSRFToken: a.Sessions.CSRFToken(ctx), Flash: a.Sessions.PopFlash(ctx)}
	if id := a.Sessions.UserID(ctx); id != 0 {
		u, err := a.Queries.GetUserByID(ctx, id)
		if err != nil {
			a.Logger.Error("Failed to get user by id", "err", err)
		} else {
			v.User = &u
			v.Theme = string(u.ThemePreference)
		}
	}
	return v
}

// render writes the status, then each component in order. A handler passes
// more than one when a mutation carries an out-of-band swap alongside its
// primary fragment — htmx pulls any hx-swap-oob element out of the combined
// body regardless of where it falls. A failure is logged rather than
// answered with a 500, since the status line is already on the wire.
func (a *App) render(w http.ResponseWriter, r *http.Request, status int, cs ...templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	for _, c := range cs {
		if err := c.Render(r.Context(), w); err != nil {
			a.Logger.Error("render component", "err", err, "path", r.URL.Path)
			return
		}
	}
}

func isHTMXRequest(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

// redirect sends an htmx caller via HX-Redirect and everyone else via 303, so
// a form works with or without JavaScript.
func redirect(w http.ResponseWriter, r *http.Request, target string) {
	if isHTMXRequest(r) {
		w.Header().Set("HX-Redirect", target)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (a *App) notFound(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "not found", http.StatusNotFound)
}

func (a *App) forbidden(w http.ResponseWriter, _ *http.Request, msg string) {
	http.Error(w, msg, http.StatusForbidden)
}

func (a *App) serverError(w http.ResponseWriter, r *http.Request, err error) {
	a.Logger.Error("handler error", "err", err, "path", r.URL.Path)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}
