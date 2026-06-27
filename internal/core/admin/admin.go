// Package admin contains the admin global domain types and password hashing
// utilities used by the /setup wizard and login flow (issue #16).
//
// The admin global is a *bootstrap owner* — there is only one such account,
// created via the /setup wizard when the database has zero admins. Subsequent
// /setup calls must fail (404). The password is hashed with Argon2id using the
// OWASP 2024 recommended parameters (64 MiB, 3 iters, 2 parallelism, PHC format).
package admin

import (
	"time"

	"github.com/google/uuid"
)

// AdminGlobal represents the single bootstrap admin account (issue #16).
// There is at most one row in the admin_globals table at any time.
type AdminGlobal struct {
	ID           uuid.UUID
	Email        string
	PasswordHash string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
