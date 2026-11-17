package providerproduct_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/providerproduct"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcecollector"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceconnectors"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceingest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type productTransport func(*http.Request) (*http.Response, error)

func (fn productTransport) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestStandaloneRestrictedCollectorAndOperationalHealth(t *testing.T) {
	db := productDB(t)
	ctx := context.Background()
	if e := providerproduct.Bootstrap(ctx, db); e != nil {
		t.Fatal(e)
	}
	login := "provider_collector_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	password := strings.ReplaceAll(uuid.NewString()+uuid.NewString(), "-", "")
	if e := providerproduct.ProvisionLogin(ctx, db, login, password, "screening_provider_collector"); e != nil {
		t.Fatal(e)
	}
	cfg := db.Config()
	cfg.ConnConfig.User = login
	cfg.ConnConfig.Password = password
	collectorDB, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		collectorDB.Close()
		if _, e := db.Exec(context.Background(), "DROP OWNED BY "+pgx.Identifier{login}.Sanitize()+"; DROP ROLE "+pgx.Identifier{login}.Sanitize()); e != nil {
			t.Error(e)
		}
	})
	if rows, e := collectorDB.Query(ctx, `SELECT digest FROM provider_api.credentials`); e == nil {
		rows.Close()
		t.Fatal("collector could read API credentials")
	}
	spec, e := sourceconnectors.Lookup("ofac-sdn")
	if e != nil {
		t.Fatal(e)
	}
	key := sourceingest.Key{Dataset: "ofac-product-fixture"}
	if e = (sourcecollector.Pipeline{DB: collectorDB}).Register(ctx, key, spec); e != nil {
		t.Fatal(e)
	}
	artifact, e := os.ReadFile("../../testdata/source-fixtures/ofac-sdn.xml")
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	collector := sourcecollector.Collector{DB: collectorDB, Dir: t.TempDir(), Fetcher: sourceconnectors.Fetcher{Client: &http.Client{Transport: productTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(artifact)), ContentLength: int64(len(artifact)), Request: r}, nil
	})}}}
	if work, e := collector.RunOne(ctx); e != nil || work || calls != 0 {
		t.Fatal("unapproved source collected", work, calls, e)
	}
	// Fictional parser fixture approval exercises the technical gate only.
	if _, e = db.Exec(ctx, `UPDATE screening.source_connections SET enabled=true,rights_state='approved',rights_review_reference='Authored fixture approval only' WHERE dataset_id=$1`, key.Dataset); e != nil {
		t.Fatal(e)
	}
	if work, e := collector.RunOne(ctx); e != nil || !work || calls != 1 {
		t.Fatal("independent collection failed", work, calls, e)
	}
	var published bool
	if e = db.QueryRow(ctx, `SELECT active_generation IS NOT NULL FROM screening.source_datasets WHERE owner_scope=$1 AND id=$2`, key.Owner, key.Dataset).Scan(&published); e != nil || !published {
		t.Fatal("standalone publication failed", e)
	}
	health, e := collector.Health(ctx, 5*time.Minute)
	if e != nil || health.Status != "healthy" {
		t.Fatal("standalone health", health, e)
	}
	var diagnostics struct {
		Supported bool `json:"platform_workflows_supported"`
		Sources   int  `json:"sources"`
	}
	if json.Unmarshal(health.Diagnostics, &diagnostics) != nil || diagnostics.Supported || diagnostics.Sources != 1 {
		t.Fatal("standalone diagnostics fabricated platform workflow support")
	}
	store := sourceingest.Store{DB: collectorDB, GlobalOnly: true}
	if _, e = store.RecoverInterrupted(ctx, 10); e != nil {
		t.Fatal(e)
	}
	if _, e = store.CleanupUnpublished(ctx); e != nil {
		t.Fatal("source cleanup required fraud workflows", e)
	}
	if _, e = store.CleanupArtifacts(ctx); e != nil {
		t.Fatal("artifact cleanup required fraud workflows", e)
	}
}
