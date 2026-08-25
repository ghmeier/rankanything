package app

import (
	"net/http"

	"golang.org/x/time/rate"
)

func (a *App) registerAuthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /register", a.handleRegisterForm)
	mux.HandleFunc("GET /login", a.handleLoginForm)
	mux.HandleFunc("POST /logout", a.handleLogout)
	mux.HandleFunc("GET /verify", a.handleVerifyEmail)
	mux.HandleFunc("GET /forgot-password", a.handleForgotPasswordForm)
	mux.HandleFunc("GET /reset-password", a.handleResetPasswordForm)
	mux.HandleFunc("POST /reset-password", a.handleResetPassword)

	if a.RateLimiter != nil {
		loginLimit := a.RateLimiter.Limit(rate.Limit(10.0/60), 10)
		registerLimit := a.RateLimiter.Limit(rate.Limit(5.0/60), 5)
		forgotLimit := a.RateLimiter.Limit(rate.Limit(3.0/60), 3)
		resendLimit := a.RateLimiter.Limit(rate.Limit(3.0/60), 3)

		mux.Handle("POST /login", loginLimit(http.HandlerFunc(a.handleLogin)))
		mux.Handle("POST /register", registerLimit(http.HandlerFunc(a.handleRegister)))
		mux.Handle("POST /forgot-password", forgotLimit(http.HandlerFunc(a.handleForgotPassword)))
		mux.Handle("POST /resend-verification", resendLimit(a.RequireUser(http.HandlerFunc(a.handleResendVerification))))
	} else {
		mux.HandleFunc("POST /login", a.handleLogin)
		mux.HandleFunc("POST /register", a.handleRegister)
		mux.HandleFunc("POST /forgot-password", a.handleForgotPassword)
		mux.Handle("POST /resend-verification", a.RequireUser(http.HandlerFunc(a.handleResendVerification)))
	}
}
