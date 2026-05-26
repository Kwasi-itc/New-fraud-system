package ports

import "context"

type IngestionReader interface {
	GetServiceStatus(ctx context.Context) (string, error)
}
