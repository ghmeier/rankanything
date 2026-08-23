package app

import "net/http"

// registerAuthRoutes mounts registration, login, and logout. feat/auth-flows
// (wave 3) extends this file with email verification and password-reset
// routes.
func (a *App) registerAuthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /register", a.handleRegisterForm)
	mux.HandleFunc("POST /register", a.handleRegister)
	mux.HandleFunc("GET /login", a.handleLoginForm)
	mux.HandleFunc("POST /login", a.handleLogin)
	mux.HandleFunc("POST /logout", a.handleLogout)
}
