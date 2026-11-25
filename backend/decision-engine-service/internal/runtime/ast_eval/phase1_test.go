package ast_eval

import (
	"context"
	"errors"
	"testing"
	"time"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type blockingAggregateReader struct {
	started chan struct{}
	release chan struct{}
}

func (b *blockingAggregateReader) GetRecord(context.Context, string, string, string) (ports.TenantRecord, error) {
	return ports.TenantRecord{}, nil
}
func (b *blockingAggregateReader) ListRecords(context.Context, string, string, int) ([]ports.TenantRecord, error) {
	return nil, nil
}
func (b *blockingAggregateReader) QueryRecords(context.Context, string, string, string, string, int) ([]ports.TenantRecord, error) {
	return nil, nil
}
func (b *blockingAggregateReader) AggregateRecords(ctx context.Context, _ string, _ ports.AggregateQuery) (any, error) {
	select {
	case b.started <- struct{}{}:
	default:
	}
	select {
	case <-b.release:
		return int64(1), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestAcquireAggregateRemoteSlotReturnsOverloadedOnMaxWait(t *testing.T) {
	reader := &blockingAggregateReader{
		started: make(chan struct{}, 2),
		release: make(chan struct{}),
	}

	node := domainast.Node{
		Function: "Aggregator",
		NamedChildren: map[string]domainast.Node{
			"tableName":  {Constant: "transactions"},
			"fieldName":  {Constant: "object_id"},
			"aggregator": {Constant: "COUNT"},
		},
	}
	rt1 := Runtime{
		TenantID:                   "t1",
		ObjectType:                 "transactions",
		TenantDataReader:           reader,
		AggregatePushdownMode:      AggregatePushdownModeEnabled,
		AggregateRemoteConcurrency: 1,
		AggregateRemoteMaxWait:     50 * time.Millisecond,
	}

	done := make(chan error, 1)
	go func() {
		_, err := EvaluateNode(context.Background(), node, rt1)
		done <- err
	}()
	<-reader.started

	rt2 := rt1
	rt2.AggregateRemoteMaxWait = 15 * time.Millisecond
	_, err := EvaluateNode(context.Background(), node, rt2)
	if !errors.Is(err, ErrAggregateOverloaded) {
		t.Fatalf("second EvaluateNode error = %v, want ErrAggregateOverloaded", err)
	}
	close(reader.release)
	if err := <-done; err != nil {
		t.Fatalf("first EvaluateNode error = %v", err)
	}
}
