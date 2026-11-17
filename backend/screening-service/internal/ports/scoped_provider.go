package ports

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/provideraccess"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
)

// ScopedScreeningProvider is the explicit internal port. The old port remains a
// narrow compatibility boundary for existing HTTP adapters and persisted jobs.
type ScopedScreeningProvider interface {
	Search(context.Context, provideraccess.Scope, screening.SearchRequest) (screening.ProviderResult, error)
	Enrich(context.Context, provideraccess.Scope, sourcedata.CandidateRef) (screening.EnrichmentResult, error)
	GetCatalog(context.Context, provideraccess.Scope, sourcedata.Operation) (screening.DatasetCatalog, error)
	GetFreshness(context.Context, provideraccess.Scope, sourcedata.Operation) (screening.DatasetFreshness, error)
	GetDatasetDelta(context.Context, provideraccess.Scope, sourcedata.Operation) (screening.DatasetDelta, error)
}
