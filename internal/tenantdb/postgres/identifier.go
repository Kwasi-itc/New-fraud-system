package postgres

import "github.com/jackc/pgx/v5"

func sanitizeIdentifier(parts ...string) string {
	return pgx.Identifier(parts).Sanitize()
}
