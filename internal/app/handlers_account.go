package app

import (
	"errors"
	"net/http"

	"github.com/ghmeier/rankanything/internal/db"
	"github.com/ghmeier/rankanything/internal/services"
	"github.com/ghmeier/rankanything/internal/ui"
)

// handleAccountPage is GET /account. RequireUser gates this route, so
// base().User is always set here.
func (a *App) handleAccountPage(w http.ResponseWriter, r *http.Request) {
	base := a.base(r)
	props := ui.AccountProps{
		CSRFToken: base.CSRFToken,
		LoggedIn:  base.User != nil,
		Flash:     base.Flash,
		Email:     base.User.Email,
		Theme:     base.Theme,
	}
	if err := renderComponent(w, r, http.StatusOK, ui.AccountPage(props)); err != nil {
		a.serverError(w, r, err)
	}
}

// handleUpdateTheme is POST /account/theme. It responds with just the
// ThemeSettings fragment, which the form targets for its own htmx swap — the
// same pattern inline tier editing uses for a control that lives inside a
// bigger page.
func (a *App) handleUpdateTheme(w http.ResponseWriter, r *http.Request) {
	userID := a.Sessions.UserID(r.Context())

	user, err := a.UserSvc.UpdateThemePreference(r.Context(), services.UpdateThemePreferenceRequest{
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

	if err := renderComponent(w, r, http.StatusOK, ui.ThemeSettings(string(user.ThemePreference))); err != nil {
		a.serverError(w, r, err)
	}
}
