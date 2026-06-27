package admin

import "errors"

var (
	ErrNotFound           = errors.New("not found")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUserDisabled       = errors.New("user is disabled")
	ErrUserNotProvisioned = errors.New("user not provisioned")
	ErrTooManyAttempts    = errors.New("too many login attempts")
	ErrSessionExpired     = errors.New("session expired")
	ErrEmailNotVerified   = errors.New("email not verified")
	ErrPasswordTooLong    = errors.New("password too long")
	ErrInvalidEmail       = errors.New("invalid email")
	ErrEmailTaken         = errors.New("email already taken")
	ErrInvalidTenantName  = errors.New("invalid tenant name")
	ErrInvalidSlug        = errors.New("invalid slug")
	ErrTenantExists       = errors.New("tenant already exists")
	ErrRoleNotFound       = errors.New("role not found")
	ErrInvalidRoleName    = errors.New("invalid role name")
	ErrInvalidSecret      = errors.New("invalid secret")
	ErrSelfLockout        = errors.New("cannot disable your own user")
	ErrProtectedRole      = errors.New("role is protected and cannot be modified")
	ErrStateMismatch      = errors.New("state mismatch")
	ErrNotAuthorized      = errors.New("not authorized")
)
