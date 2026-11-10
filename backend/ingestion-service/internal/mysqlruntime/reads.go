package mysqlruntime

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/store/mysql"
	"github.com/google/uuid"
)

var readQueryBudget = store.RecordQueryLimits{Parameters: 2048, ParameterBytes: 2 << 20, SQLBytes: 1 << 20}

func resultBudget(n int) store.RecordResultLimits {
	return store.RecordResultLimits{FieldBytes: 16 << 10, TransferBytes: 2 << 20, RowBytes: 1 << 20, PageBytes: 4 << 20, Rows: n}
}
func readBoundary(e error) error {
	if e == nil {
		return nil
	}
	switch {
	case errors.Is(e, store.ErrRecordResultLimit), errors.Is(e, store.ErrRecordQueryLimit):
		return service.ErrRequestLimits
	case errors.Is(e, service.ErrBadInput), errors.Is(e, store.ErrRecordCount), errors.Is(e, store.ErrRecordSum), errors.Is(e, store.ErrRecordMembership), errors.Is(e, store.ErrRecordPredicate), errors.Is(e, store.ErrRecordFilter), errors.Is(e, store.ErrRecordAggregate), errors.Is(e, store.ErrRecordLookup):
		return service.ErrBadInput
	}
	return boundary(e)
}
func (s *Service) read(ctx context.Context, tenant uuid.UUID, name string, call func(context.Context, *sql.Tx, *store.RecordProjection) error) error {
	if !s.multiTenantLocal() && (tenant != s.Config.TenantID || name != s.Config.ObjectType) {
		return service.ErrForbidden
	}
	contracts, e := store.NewIngestionContractStore(s.DB, s.Config.Database, s.Config.ContractBytes)
	if e != nil {
		return readBoundary(e)
	}
	return readBoundary(contracts.WithRecordRead(ctx, tenant, name, s.Config.Token, nil, call))
}
func (s *Service) GetRecord(ctx context.Context, tenant uuid.UUID, name, id string) (service.RecordLookupResult, error) {
	var fields map[string]any
	e := s.read(ctx, tenant, name, func(ctx context.Context, tx *sql.Tx, p *store.RecordProjection) error {
		var e error
		fields, e = p.ReadCurrentExact(ctx, tx, id, resultBudget(1), readQueryBudget)
		return e
	})
	if e != nil {
		return service.RecordLookupResult{}, e
	}
	return service.RecordLookupResult{ObjectID: id, ObjectType: name, Fields: fields}, nil
}
func (s *Service) records(ctx context.Context, tenant uuid.UUID, name string, limit int, filter *ingestion.AggregateFilter) ([]service.RecordLookupResult, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		return nil, service.ErrRequestLimits
	}
	var records []map[string]any
	e := s.read(ctx, tenant, name, func(ctx context.Context, tx *sql.Tx, p *store.RecordProjection) error {
		normalized, e := p.NormalizeReadFilter(filter)
		if e != nil {
			return e
		}
		records, e = p.ReadCurrentPage(ctx, tx, normalized, resultBudget(limit), readQueryBudget)
		return e
	})
	if e != nil {
		return nil, e
	}
	out := make([]service.RecordLookupResult, len(records))
	for i, r := range records {
		out[i] = service.RecordLookupResult{ObjectID: fmt.Sprint(r["object_id"]), ObjectType: name, Fields: r}
	}
	wire, e := json.Marshal(out)
	if e != nil {
		return nil, service.ErrUnavailable
	}
	if len(wire) > 4<<20 {
		return nil, service.ErrRequestLimits
	}
	return out, nil
}
func (s *Service) ListRecords(ctx context.Context, t uuid.UUID, n string, limit int) (service.RecordListResult, error) {
	r, e := s.records(ctx, t, n, limit, nil)
	return service.RecordListResult{Records: r}, e
}
func (s *Service) QueryRecords(ctx context.Context, t uuid.UUID, n, field, value string, limit int) (service.RecordQueryResult, error) {
	r, e := s.records(ctx, t, n, limit, &ingestion.AggregateFilter{Kind: "predicate", Field: field, Op: "eq", Value: value})
	return service.RecordQueryResult{Records: r}, e
}
func (s *Service) AggregateRecords(ctx context.Context, t uuid.UUID, q ingestion.AggregateQuery) (service.AggregateResult, error) {
	if e := ingestion.ValidateAggregateQuery(q); e != nil {
		return service.AggregateResult{}, service.ErrBadInput
	}
	var value any
	e := s.read(ctx, t, q.ObjectType, func(ctx context.Context, tx *sql.Tx, p *store.RecordProjection) error {
		f, e := p.NormalizeReadFilter(q.Filter)
		if e != nil {
			return e
		}
		switch strings.ToLower(strings.TrimSpace(q.Aggregate)) {
		case "count":
			value, e = p.CountRecords(ctx, tx, q.Field, f, readQueryBudget)
		case "sum":
			value, e = p.SumRecords(ctx, tx, q.Field, f, readQueryBudget)
		case "avg":
			value, e = p.AverageRecords(ctx, tx, q.Field, f, readQueryBudget)
		case "min":
			value, e = p.MinRecords(ctx, tx, q.Field, f, readQueryBudget)
		case "max":
			value, e = p.MaxRecords(ctx, tx, q.Field, f, readQueryBudget)
		case "count_distinct":
			value, e = p.CountDistinctRecords(ctx, tx, q.Field, f, readQueryBudget)
		default:
			return service.ErrBadInput
		}
		return e
	})
	if e != nil {
		return service.AggregateResult{}, e
	}
	if decimal, ok := value.(store.RecordDecimal); ok {
		value = json.Number(decimal)
	}
	return service.AggregateResult{Value: value}, nil
}
