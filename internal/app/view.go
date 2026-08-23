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
}

func (a *App) base(r *http.Request) BaseView {
	ctx := r.Context()
	v := BaseView{CSRFToken: a.Sessions.CSRFToken(ctx), Flash: a.Sessions.PopFlash(ctx)}
	if id := a.Sessions.UserID(ctx); id != 0 {
		if u, err := a.Queries.GetUserByID(ctx, id); err == nil {
			v.User = &u
		}
	}
	return v
}

// renderComponent writes the status line and renders a templ component
// straight to w. templ.Component.Render has no notion of a status code, so
// the header has to be set first — the same ordering render.Renderer.exec
// uses for html/template partials.
func renderComponent(w http.ResponseWriter, r *http.Request, status int, c templ.Component) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	return c.Render(r.Context(), w)
}

// isHTMXRequest reports whether the request wants a fragment rather than a
// whole page.
func isHTMXRequest(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

// empty answers with a status and no body — what a deletion or a refused
// mutation returns to htmx.
func empty(w http.ResponseWriter, status int) {
	w.WriteHeader(status)
}

func (a *App) notFound(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "not found", http.StatusNotFound)
}

func (a *App) serverError(w http.ResponseWriter, r *http.Request, err error) {
	a.Logger.Error("handler error", "err", err, "path", r.URL.Path)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}
