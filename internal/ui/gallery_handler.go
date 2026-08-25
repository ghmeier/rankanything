package ui

import "net/http"

func ComponentsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := ComponentsGallery().Render(r.Context(), w); err != nil {
		http.Error(w, "failed to render components", http.StatusInternalServerError)
	}
}
