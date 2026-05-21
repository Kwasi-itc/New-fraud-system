package service

import "errors"

var (
	ErrIdempotencyKeyReused       = errors.New("idempotency key reused with different payload")
)
