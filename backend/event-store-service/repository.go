// Package eventstore exposes the shared ClickHouse event repository used by
// storage-owning services. The HTTP server under cmd/server is a compatibility
// adapter; normal ingestion and decision paths import this package directly.
package eventstore

import internal "github.com/Kwasi-itc/New-fraud-system/backend/event-store-service/internal/eventstore"

type Config = internal.Config
type Repository = internal.Repository
type Event = internal.Event
type FieldContract = internal.FieldContract
type TableContract = internal.TableContract
type RecordRequest = internal.RecordRequest
type AggregateRequest = internal.AggregateRequest
type AggregateFilter = internal.AggregateFilter

const (
	AggregationModeProjectionOnly = internal.AggregationModeProjectionOnly
	AggregationModeAdaptiveCache  = internal.AggregationModeAdaptiveCache
	AggregationModeTieredSummary  = internal.AggregationModeTieredSummary
	AggregationModeAlwaysOnline   = internal.AggregationModeAlwaysOnline
)

var NewRepository = internal.NewRepository
var ValidateAggregateRequest = internal.ValidateAggregateRequest
var ErrAggregationDeferred = internal.ErrAggregationDeferred
var ErrAggregationSkipped = internal.ErrAggregationSkipped
