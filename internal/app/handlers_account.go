package app

import (
	"errors"
	"net/http"

	"github.com/ghmeier/rankanything/internal/db"
	"github.com/ghmeier/rankanything/internal/services"
	"github.com/ghmeier/rankanything/internal/ui"
)

func (a *App) handleAccountPage(w http.ResponseWriter, r *http.Request) {
	base := a.base(r)
	props := ui.AccountProps{
		CSRFToken: base.CSRFToken,
		LoggedIn:  base.User != nil,
		Flash:     base.Flash,
		Email:     base.User.Email,
		Theme:     base.Theme,
	}
	a.render(w, r, http.StatusOK, ui.AccountPage(props))
}

func (a *App) handleUpdateTheme(w http.ResponseWriter, r *http.Request) {
	userID := a.Sessions.UserID(r.Context())

	_, err := a.UserSvc.UpdateThemePreference(r.Context(), services.UpdateThemePreferenceRequest{
		UserID:     userID,
		Preference: db.UserThemePreference(r.FormValue("theme")),
	})
	if err != nil {
		if errors.Is(err, services.ErrInvalidThemePreference) {
			http.Error(w, "invalid theme", http.StatusBadRequest)
			return
		}
		a.serverError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusOK)
}
