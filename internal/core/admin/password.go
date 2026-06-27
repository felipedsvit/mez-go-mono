package admin

import "context"

type PasswordHasher interface {
	Hash(ctx context.Context, plaintext string) (string, error)
	Verify(ctx context.Context, encoded, plaintext string) (bool, error)
}
