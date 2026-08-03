package service

import (
	"context"
	"sync/atomic"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type tenantDataReadMetrics struct {
	getRecordCount            atomic.Uint64
	getRecordErrorCount       atomic.Uint64
	listRecordsCount          atomic.Uint64
	listRecordsErrorCount     atomic.Uint64
	listRecordsLimitTotal     atomic.Uint64
	listRecordsMaxLimit       atomic.Uint64
	queryRecordsCount         atomic.Uint64
	queryRecordsErrorCount    atomic.Uint64
	queryRecordsLimitTotal    atomic.Uint64
	queryRecordsMaxLimit      atomic.Uint64
	aggregateRecordsCount     atomic.Uint64
	aggregateRecordsErrorCount atomic.Uint64
}

type TenantDataReadMetrics struct {
	GetRecordCount         uint64 `json:"get_record_count"`
	GetRecordErrorCount    uint64 `json:"get_record_error_count"`
	ListRecordsCount       uint64 `json:"list_records_count"`
	ListRecordsErrorCount  uint64 `json:"list_records_error_count"`
	ListRecordsLimitTotal  uint64 `json:"list_records_limit_total"`
	ListRecordsMaxLimit    uint64 `json:"list_records_max_limit"`
	QueryRecordsCount      uint64 `json:"query_records_count"`
	QueryRecordsErrorCount uint64 `json:"query_records_error_count"`
	QueryRecordsLimitTotal uint64 `json:"query_records_limit_total"`
	QueryRecordsMaxLimit   uint64 `json:"query_records_max_limit"`
	AggregateRecordsCount  uint64 `json:"aggregate_records_count"`
	AggregateRecordsErrorCount uint64 `json:"aggregate_records_error_count"`
}

func (m *tenantDataReadMetrics) snapshot() TenantDataReadMetrics {
	if m == nil {
		return TenantDataReadMetrics{}
	}
	return TenantDataReadMetrics{
		GetRecordCount:            m.getRecordCount.Load(),
		GetRecordErrorCount:       m.getRecordErrorCount.Load(),
		ListRecordsCount:          m.listRecordsCount.Load(),
		ListRecordsErrorCount:     m.listRecordsErrorCount.Load(),
		ListRecordsLimitTotal:     m.listRecordsLimitTotal.Load(),
		ListRecordsMaxLimit:       m.listRecordsMaxLimit.Load(),
		QueryRecordsCount:         m.queryRecordsCount.Load(),
		QueryRecordsErrorCount:    m.queryRecordsErrorCount.Load(),
		QueryRecordsLimitTotal:    m.queryRecordsLimitTotal.Load(),
		QueryRecordsMaxLimit:      m.queryRecordsMaxLimit.Load(),
		AggregateRecordsCount:     m.aggregateRecordsCount.Load(),
		AggregateRecordsErrorCount: m.aggregateRecordsErrorCount.Load(),
	}
}

func (m *tenantDataReadMetrics) recordListLimit(limit int) {
	if m == nil || limit < 0 {
		return
	}
	value := uint64(limit)
	m.listRecordsLimitTotal.Add(value)
	updateMax(&m.listRecordsMaxLimit, value)
}

func (m *tenantDataReadMetrics) recordQueryLimit(limit int) {
	if m == nil || limit < 0 {
		return
	}
	value := uint64(limit)
	m.queryRecordsLimitTotal.Add(value)
	updateMax(&m.queryRecordsMaxLimit, value)
}

func updateMax(slot *atomic.Uint64, candidate uint64) {
	for {
		current := slot.Load()
		if candidate <= current {
			return
		}
		if slot.CompareAndSwap(current, candidate) {
			return
		}
	}
}

type instrumentedTenantDataReader struct {
	reader  ports.TenantDataReader
	metrics *tenantDataReadMetrics
}

func (r instrumentedTenantDataReader) GetRecord(ctx context.Context, tenantID, objectType, objectID string) (ports.TenantRecord, error) {
	r.metrics.getRecordCount.Add(1)
	record, err := r.reader.GetRecord(ctx, tenantID, objectType, objectID)
	if err != nil {
		r.metrics.getRecordErrorCount.Add(1)
	}
	return record, err
}

func (r instrumentedTenantDataReader) ListRecords(ctx context.Context, tenantID, objectType string, limit int) ([]ports.TenantRecord, error) {
	r.metrics.listRecordsCount.Add(1)
	r.metrics.recordListLimit(limit)
	records, err := r.reader.ListRecords(ctx, tenantID, objectType, limit)
	if err != nil {
		r.metrics.listRecordsErrorCount.Add(1)
	}
	return records, err
}

func (r instrumentedTenantDataReader) QueryRecords(ctx context.Context, tenantID, objectType, fieldName, value string, limit int) ([]ports.TenantRecord, error) {
	r.metrics.queryRecordsCount.Add(1)
	r.metrics.recordQueryLimit(limit)
	records, err := r.reader.QueryRecords(ctx, tenantID, objectType, fieldName, value, limit)
	if err != nil {
		r.metrics.queryRecordsErrorCount.Add(1)
	}
	return records, err
}

func (r instrumentedTenantDataReader) AggregateRecords(ctx context.Context, tenantID string, query ports.AggregateQuery) (any, error) {
	r.metrics.aggregateRecordsCount.Add(1)
	value, err := r.reader.AggregateRecords(ctx, tenantID, query)
	if err != nil {
		r.metrics.aggregateRecordsErrorCount.Add(1)
	}
	return value, err
}
