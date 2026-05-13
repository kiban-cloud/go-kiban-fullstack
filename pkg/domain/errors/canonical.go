package errors_domain

import "errors"

// Canonical HTTP-aligned error sentinels for HTMX error mapping.
//
// These are the eight generic categories shared across all Kiban projects.
// The mapping table in `pkg/infrastructure/http/htmx.Config` translates each
// to an HTTP status (4xx/5xx) and a fragment slot.
//
// Project-specific sentinels (ErrRateLimited for one project, ErrRenapoUnavailable
// for another) live in the project's own errors package and are matched via
// the optional `ProjectMapper` callback on the httperrors Config.
//
// Usecase sentinels should wrap one of these via stdlib fmt.Errorf so that
// errors.Is traverses the chain to the canonical match:
//
//	var ErrEmailOrPasswordInvalid = fmt.Errorf("%w: correo o contraseña incorrectos", ErrUnauthorized)
//
// `errors.Is(err, login.ErrEmailOrPasswordInvalid)` still works (pointer
// equality on the sentinel) AND `errors.Is(err, ErrUnauthorized)` matches.
var (
	ErrInvalidInput               = errors.New("invalid input")
	ErrValidation                 = errors.New("validation failed")
	ErrNotFound                   = errors.New("not found")
	ErrUnauthorized               = errors.New("unauthorized")
	ErrForbidden                  = errors.New("forbidden")
	ErrAlreadyExists              = errors.New("already exists")
	ErrConflict                   = errors.New("conflict")
	ErrExternalServiceUnavailable = errors.New("external service unavailable")
)
