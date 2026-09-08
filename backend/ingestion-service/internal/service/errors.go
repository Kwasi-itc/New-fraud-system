package service

import (
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/ports"
)

var (
	ErrIdempotencyKeyReused       = errors.New("idempotency key reused with different payload")
	ErrAggregateFactUnavailable   = errors.New("required aggregate fact update could not be confirmed")
	ErrFactRecordMutationRejected = ports.ErrAggregateFactMutationUnsupported
)
