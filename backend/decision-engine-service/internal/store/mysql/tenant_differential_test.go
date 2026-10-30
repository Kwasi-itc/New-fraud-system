package mysql

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	ingestionclient "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/clients/ingestion"
	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	asteval "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/runtime/ast_eval"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/postgres"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/testsupport"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func walletRuleAST() domainast.Node {
	filter := func(field, op string) domainast.Node {
		return domainast.Node{Function: "Filter", NamedChildren: map[string]domainast.Node{"tableName": {Constant: "wallets"}, "fieldName": {Constant: field}, "operator": {Constant: op}, "value": {Function: "Payload", Children: []domainast.Node{{Constant: field}}}}}
	}
	return domainast.Node{Function: "gte", Children: []domainast.Node{{Function: "Aggregator", NamedChildren: map[string]domainast.Node{"tableName": {Constant: "wallets"}, "fieldName": {Constant: "object_id"}, "aggregator": {Constant: "COUNT"}, "filters": {Function: "List", Children: []domainast.Node{filter("wallet_number_hash", "="), filter("user_id", "!=")}}}}, {Constant: 1}}}
}
func TestWalletRulePostgresMySQLAndHTTPTransportDifferential(t *testing.T) {
	ctx := context.Background()
	dsn := testsupport.PostgresDatabaseURL(t)
	pg, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	f := tenantRegistryFixture(t)
	schema := "tenant_" + stripUUID(f.tenant)
	quoted := pgx.Identifier{schema}.Sanitize()
	table := pgx.Identifier{schema, "wallets"}.Sanitize()
	if _, err := pg.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.Exec(ctx, "CREATE TABLE "+table+"(object_id TEXT PRIMARY KEY,updated_at TIMESTAMPTZ NOT NULL,wallet_number_hash TEXT NOT NULL,user_id TEXT NULL,amount INT NOT NULL,verified BOOLEAN NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	source := postgres.NewTenantDataReader(pg, nil).WithModel(f.tenant.String(), f.model)
	target := f.reader.WithModel(f.tenant.String(), f.model)
	// Real source SQL plus the real decision HTTP client, with a contract-only
	// endpoint. This is not a full ingestion-service deployment/parity claim.
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/tenants/"+f.tenant.String()+"/query/aggregate" {
			http.Error(w, "wrong tenant or path", 404)
			return
		}
		var query ports.AggregateQuery
		decoder := json.NewDecoder(r.Body)
		decoder.UseNumber()
		if err := decoder.Decode(&query); err != nil {
			http.Error(w, "invalid query", 400)
			return
		}
		value, err := source.AggregateRecords(r.Context(), f.tenant.String(), query)
		if err != nil {
			http.Error(w, "source query failed", 500)
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"value": value}); err != nil {
			t.Error(err)
		}
	}))
	defer endpoint.Close()
	remote := ingestionclient.NewHTTPClient(endpoint.URL, time.Second)
	inputs := []struct {
		id, hash string
		owner    any
		amount   int
	}{{"same", "HASH", "U1", 100}, {"null-owner", "HASH", nil, 80}, {"different-hash", "OTHER", "U2", 50}}
	insert := func(v struct {
		id, hash string
		owner    any
		amount   int
	}) {
		t.Helper()
		f.insert(t, v.id, v.hash, v.owner, v.amount)
		if _, err := pg.Exec(ctx, "INSERT INTO "+table+" VALUES($1,$2,$3,$4,$5,true)", v.id, time.Now().UTC(), v.hash, v.owner, v.amount); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range inputs {
		insert(v)
	}
	node := walletRuleAST()
	evaluate := func(want bool) {
		t.Helper()
		for _, reader := range []ports.TenantDataReader{source, target, remote} {
			value, err := asteval.EvaluateNode(ctx, node, asteval.Runtime{TenantID: f.tenant.String(), ObjectID: "incoming", ObjectType: "wallets", Fields: map[string]any{"wallet_number_hash": "HASH", "user_id": "U1"}, Model: &f.model, TenantDataReader: reader, AggregatePushdownMode: asteval.AggregatePushdownModeStrict})
			if err != nil || value != want {
				t.Fatalf("reader %T: outcome=%v error=%v", reader, value, err)
			}
		}
	}
	evaluate(false)
	insert(struct {
		id, hash string
		owner    any
		amount   int
	}{"other-owner", "HASH", "U2", 20})
	evaluate(true)
	// An entirely separate tenant's records cannot enter a registry count.
	if _, err := target.AggregateRecords(ctx, uuid.NewString(), ports.AggregateQuery{ObjectType: "wallets", Aggregate: "count", Field: "object_id"}); err == nil {
		t.Fatal("cross tenant reader accepted")
	}
}
func stripUUID(id uuid.UUID) string {
	raw := id.String()
	return raw[0:8] + raw[9:13] + raw[14:18] + raw[19:23] + raw[24:36]
}
