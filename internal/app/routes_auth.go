package app

import "net/http"

// registerAuthRoutes mounts registration, login, logout, email verification,
// and password reset.
func (a *App) registerAuthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /register", a.handleRegisterForm)
	mux.HandleFunc("POST /register", a.handleRegister)
	mux.HandleFunc("GET /login", a.handleLoginForm)
	mux.HandleFunc("POST /login", a.handleLogin)
	mux.HandleFunc("POST /logout", a.handleLogout)

	mux.HandleFunc("GET /verify", a.handleVerifyEmail)
	mux.Handle("POST /resend-verification", a.RequireUser(http.HandlerFunc(a.handleResendVerification)))

	mux.HandleFunc("GET /forgot-password", a.handleForgotPasswordForm)
	mux.HandleFunc("POST /forgot-password", a.handleForgotPassword)
	mux.HandleFunc("GET /reset-password", a.handleResetPasswordForm)
	mux.HandleFunc("POST /reset-password", a.handleResetPassword)
}
