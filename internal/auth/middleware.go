package auth

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"runtime/debug"
	"slices"
)

// CSRF rejects mutating requests whose token does not match the session's.
// htmx sends the token via the X-CSRF-Token header (see the layout's hx-headers);
func CSRF(s *Sessions) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}

			want := s.CSRFToken(r.Context())
			got := r.Header.Get("X-CSRF-Token")
			if subtle.ConstantTimeCompare([]byte(want), []byte(got)) != 1 {
				http.Error(w, "invalid CSRF token", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Recover turns a panic into a 500 plus a logged stack trace.
func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic", "err", rec, "stack", string(debug.Stack()), "path", r.URL.Path)
					http.Error(w, "internal server error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// RequestLog emits one structured line per request.
func RequestLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)
			logger.Info("request", "method", r.Method, "path", r.URL.Path, "status", sw.status,
				"htmx", r.Header.Get("HX-Request") == "true")
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Chain applies middlewares outermost-first.
func Chain(h http.Handler, mw ...func(http.Handler) http.Handler) http.Handler {
	for _, middleware := range slices.Backward(mw) {
		h = middleware(h)
	}
	return h
}
