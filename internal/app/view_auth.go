package app

// AuthView backs the login and register pages.
type AuthView struct {
	BaseView
	Email string
	Next  string
	Error string

	// EmailAlreadyRegistered is set instead of Error when registration fails
	// on services.ErrEmailAlreadyRegistered. auth_form.html renders it as a
	// dedicated message offering both "log in" and "reset your password" as
	// the next step, rather than Error's plain text, so the page never has
	// to interpolate a trusted link through an auto-escaped string field.
	EmailAlreadyRegistered bool
}
