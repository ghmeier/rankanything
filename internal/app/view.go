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
	// Theme is User.ThemePreference as a string, or "" for a signed-out
	// visitor — see LayoutProps.Theme for what that does to the rendered
	// <html> tag.
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

// renderComponent writes the appropriate header and status, then renders
// each templ component in order. A handler passes more than one when a
// mutation carries an out-of-band swap alongside its primary fragment (see
// App.boardVersionActionsOOB) — htmx pulls any hx-swap-oob element out of
// the combined body regardless of where it falls, so concatenating them is
// enough.
func renderComponent(w http.ResponseWriter, r *http.Request, status int, cs ...templ.Component) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	for _, c := range cs {
		if err := c.Render(r.Context(), w); err != nil {
			return err
		}
	}
	return nil
}

// isHTMXRequest helps know if the request was initiated from HTMX or plain HTML so
// we can support progressive enhancement.
func isHTMXRequest(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
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
