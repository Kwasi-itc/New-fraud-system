package mysql

import (
	"context"
	"errors"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

// All table/column lists are private constants. Parameter count and readback
// size are bounded; returned rows follow input identity order, not query order.
func insertEffectBatch[T any](ctx context.Context, r authoringRepository, table, columns, selectSQL string, values [][]any, ids []string, scan func(rowScanner) (T, int64, error), identity func(T) string) (result []T, resultErr error) {
	defer func() {
		// Any failure after an earlier chunk inserted rows must poison the
		// queued unit of work, not only failures inside the final enqueue call.
		if resultErr != nil && r.effectQueues != nil && r.effectJobError != nil {
			*r.effectJobError = errors.Join(*r.effectJobError, resultErr)
		}
	}()
	if err := r.mutating(); err != nil {
		return nil, err
	}
	if len(values) != len(ids) || len(ids) > r.maxRows {
		return nil, ErrProjectionLimit
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			return nil, errors.New("duplicate effect identity")
		}
		seen[id] = true
	}
	out := make([]T, 0, len(ids))
	for start := 0; start < len(ids); start += 100 {
		end := min(start+100, len(ids))
		marks := []string{}
		args := []any{}
		readMarks := []string{}
		readArgs := []any{mysqlstore.UUID(r.tenant)}
		for i := start; i < end; i++ {
			marks = append(marks, "("+strings.TrimSuffix(strings.Repeat("?,", len(values[i])), ",")+")")
			args = append(args, values[i]...)
			readArgs = append(readArgs, values[i][0])
			readMarks = append(readMarks, "?")
		}
		if _, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+"."+table+"("+columns+") VALUES"+strings.Join(marks, ","), args...); err != nil {
			return nil, err
		}
		stored, err := platformList(ctx, r.platformRepository, selectSQL+" WHERE tenant_id=? AND id IN ("+strings.Join(readMarks, ",")+") ORDER BY id", readArgs, scan)
		if err != nil {
			return nil, err
		}
		byID := make(map[string]T, len(stored))
		for _, v := range stored {
			byID[identity(v)] = v
		}
		for _, id := range ids[start:end] {
			v, ok := byID[id]
			if !ok {
				return nil, errors.New("effect readback identity missing")
			}
			out = append(out, v)
		}
	}
	if r.effectQueues != nil {
		if err := r.enqueueCreated(ctx, table, ids); err != nil {
			return nil, err
		}
	}
	return out, nil
}
