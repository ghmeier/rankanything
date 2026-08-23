package ui

import "net/http"

// ComponentsHandler serves the development-only component gallery at
// GET /components. Registration is gated on environment in internal/app.
func ComponentsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := ComponentsGallery().Render(r.Context(), w); err != nil {
		http.Error(w, "failed to render components", http.StatusInternalServerError)
	}
}
