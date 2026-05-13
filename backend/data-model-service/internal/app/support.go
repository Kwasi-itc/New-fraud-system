package app

import (
	"time"

	"github.com/google/uuid"
)

type UUIDGenerator struct{}

func (UUIDGenerator) New() uuid.UUID {
	return uuid.New()
}

type SystemClock struct{}

func (SystemClock) Now() time.Time {
	return time.Now().UTC()
}
