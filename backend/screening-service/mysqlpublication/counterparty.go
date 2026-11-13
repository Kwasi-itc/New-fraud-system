package mysqlpublication

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/google/uuid"
)

type CounterpartyReader interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// ActiveCounterpartyExclusions returns only the bounded requested entity set.
// The caller supplies current tenant authority. Matching never uses global entries.
func ActiveCounterpartyExclusions(ctx context.Context, q CounterpartyReader, database string, tenant uuid.UUID, provider, counterparty string, entities []string, lock bool) (map[string]string, error) {
	schema, e := mysqlstore.Identifier(database)
	if e != nil || tenant == uuid.Nil || provider == "" || counterparty == "" || len(counterparty) > 512 || len(entities) > 500 {
		return nil, ErrInvalid
	}
	out := map[string]string{}
	if len(entities) == 0 {
		return out, nil
	}
	args := []any{mysqlstore.UUID(tenant), []byte(provider), []byte(counterparty)}
	for _, entity := range entities {
		args = append(args, []byte(entity))
	}
	query := "SELECT entity_id,BIN_TO_UUID(operation_id) FROM " + schema + ".screening_counterparty_exclusions WHERE tenant_id=? AND provider=? AND counterparty_id=? AND active=TRUE AND entity_id IN(" + strings.TrimSuffix(strings.Repeat("?,", len(entities)), ",") + ") ORDER BY entity_id"
	if lock {
		query += " FOR SHARE"
	}
	rows, e := q.QueryContext(ctx, query, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var entity, operation string
		if e = rows.Scan(&entity, &operation); e != nil {
			return nil, e
		}
		out[entity] = operation
	}
	return out, rows.Err()
}

func ApplyInlineCounterpartyExclusions(ctx context.Context, db *sql.DB, database string, tenant uuid.UUID, requestRaw, snapshotRaw json.RawMessage) (json.RawMessage, int, bool, error) {
	var request screening.SearchRequest
	var snapshot screening.InlineSnapshot
	if json.Unmarshal(requestRaw, &request) != nil || json.Unmarshal(snapshotRaw, &snapshot) != nil || snapshot.Validate(request) != nil {
		return nil, 0, false, ErrInvalid
	}
	if request.UniqueCounterpartyIdentifier != nil && *request.UniqueCounterpartyIdentifier != "" {
		entities := []string{}
		for _, m := range snapshot.Result.Matches {
			entities = append(entities, m.EntityID)
		}
		excluded, e := ActiveCounterpartyExclusions(ctx, db, database, tenant, request.Provider, *request.UniqueCounterpartyIdentifier, entities, false)
		if e != nil {
			return nil, 0, false, e
		}
		for i := range snapshot.Result.Matches {
			snapshot.Result.Matches[i].ExclusionOperationID = excluded[snapshot.Result.Matches[i].EntityID]
		}
	}
	count := 0
	for _, m := range snapshot.Result.Matches {
		if m.ExclusionOperationID == "" {
			count++
		}
	}
	raw, e := json.Marshal(snapshot)
	return raw, count, snapshot.Result.Partial, e
}
