package app

import "net/http"

func (a *App) registerAccountRoutes(mux *http.ServeMux) {
	mux.Handle("GET /account", a.RequireUser(http.HandlerFunc(a.handleAccountPage)))
	mux.Handle("POST /account/theme", a.RequireUser(http.HandlerFunc(a.handleUpdateTheme)))
}
