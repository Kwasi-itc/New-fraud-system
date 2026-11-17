// source-verify always creates and drops its OWN database on the explicitly
// supplied disposable server. It cannot target an existing application database.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/internalprovider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/migrations"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/provideraccess"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcecollector"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceconnectors"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceingest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "isolated source verification failed:", err)
		os.Exit(1)
	}
}
func run() (resultErr error) {
	source := flag.String("source", "", "Explicit source key (one source per run)")
	artifact := flag.String("artifact", "", "Existing private artifact path; omit only with -fetch")
	fetch := flag.Bool("fetch", false, "Explicit bounded live retrieval from the registry's official endpoint")
	repeat := flag.Bool("check-unchanged", false, "Restage the full artifact and verify snapshot idempotency")
	flag.Parse()
	if (*artifact == "") == (!*fetch) {
		return fmt.Errorf("choose exactly one of -artifact or -fetch")
	}
	spec, err := sourceconnectors.Lookup(*source)
	if err != nil {
		return err
	}
	dsn := os.Getenv("SOURCE_TEST_DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("SOURCE_TEST_DATABASE_URL must identify a dedicated disposable server; application defaults are never read")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("invalid test server configuration")
	}
	config.MaxConns = 4
	admin, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return fmt.Errorf("test server unavailable")
	}
	defer admin.Close()
	name := "screening_source_verify_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		return fmt.Errorf("cannot create isolated verification database")
	}
	defer func() {
		clean, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, e := admin.Exec(clean, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); e != nil {
			resultErr = fmt.Errorf("verification database cleanup failed; isolated name: %s", name)
		}
	}()
	config.ConnConfig.Database = name
	db, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return err
	}
	defer db.Close()
	files, err := migrations.Metadata.ReadDir("metadata")
	if err != nil {
		return err
	}
	for _, entry := range files {
		if !strings.HasSuffix(entry.Name(), ".up.sql") {
			continue
		}
		raw, e := migrations.Metadata.ReadFile("metadata/" + entry.Name())
		if e != nil {
			return e
		}
		if _, e = db.Exec(ctx, string(raw)); e != nil {
			return fmt.Errorf("isolated owner migration %s failed", entry.Name())
		}
	}
	dir, err := os.MkdirTemp("", "screening-source-verify-")
	if err != nil {
		return err
	}
	defer os.Remove(dir)
	path := *artifact
	if *fetch {
		download, e := sourceconnectors.NewFetcher().Fetch(ctx, spec, dir, "", "")
		if e != nil {
			return e
		}
		path = download.Path
		defer os.Remove(path)
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("artifact unavailable")
	}
	hash := sha256.New()
	size, err := io.Copy(hash, io.LimitReader(file, sourceconnectors.MaxArtifactBytes+1))
	file.Close()
	if err != nil || size > sourceconnectors.MaxArtifactBytes {
		return sourceconnectors.ErrLimit
	}
	key := sourceingest.Key{Dataset: spec.Key + "-verification"}
	pipeline := sourcecollector.Pipeline{DB: db}
	if err = pipeline.Register(ctx, key, spec); err != nil {
		return err
	}
	started := time.Now()
	generation, parsed, err := pipeline.StageSnapshot(ctx, key, spec, path)
	if err != nil {
		return fmt.Errorf("parser/staging failed after %d records: %w", parsed.Total, err)
	}
	publication, err := (sourceingest.Store{DB: db}).Publish(ctx, key, generation)
	if err != nil {
		return err
	}
	unchangedVerified := false
	if *repeat {
		second, _, e := pipeline.StageSnapshot(ctx, key, spec, path)
		if e != nil {
			return e
		}
		ack, e := (sourceingest.Store{DB: db}).Publish(ctx, key, second)
		if e != nil || !ack.Unchanged || ack.Generation != publication.Generation {
			return fmt.Errorf("complete snapshot idempotency failed")
		}
		unchangedVerified = true
	}
	// This private operator verification does not grant tenants access, approve
	// rights or enable collection. Public source records never reach an app DB.
	configJSON, _ := json.Marshal(sourcedata.Selection{Version: 2, Profile: sourcedata.Profile, Threshold: 100, Datasets: []sourcedata.DatasetRef{{Scope: "global", ID: key.Dataset}}})
	limit := 50
	provider := internalprovider.Provider{DB: db}
	verified, partial := map[string]bool{}, map[string]bool{}
	for _, kind := range []string{"Person", "Organization", "Vessel", "Aircraft"} {
		if parsed.ByType[kind] == 0 {
			continue
		}
		var record sourcedata.Record
		var recordJSON []byte
		if e := db.QueryRow(ctx, `SELECT record-'original_xml' FROM screening.source_records WHERE owner_scope=$1 AND dataset_id=$2 AND generation=$3 AND entity_type=$4 AND length(record->>'name')<=200 ORDER BY record_id LIMIT 1`, key.Owner, key.Dataset, publication.Generation, kind).Scan(&recordJSON); e != nil || json.Unmarshal(recordJSON, &record) != nil {
			return fmt.Errorf("no bounded %s verification sample", kind)
		}
		request := screening.SearchRequest{Provider: sourcedata.Provider, ObjectType: kind, ObjectID: "isolated-source-verification", Queries: []screening.SearchQuery{{Name: record.Name, Type: kind}}, ProviderConfig: configJSON, LimitOverride: &limit}
		result, e := provider.Search(ctx, provideraccess.GlobalOperator(), request)
		if e != nil {
			return e
		}
		found := false
		for _, match := range result.Matches {
			var evidence struct {
				Ref sourcedata.CandidateRef `json:"provenance"`
			}
			if json.Unmarshal(match.Payload, &evidence) == nil && evidence.Ref.RecordID == record.ID && evidence.Ref.Generation == publication.Generation {
				enriched, e := provider.Enrich(ctx, provideraccess.GlobalOperator(), evidence.Ref)
				if e != nil || !json.Valid(enriched.RawPayload) {
					return fmt.Errorf("%s enrichment failed", kind)
				}
				found = true
			}
		}
		if !found {
			return fmt.Errorf("published %s identity was not retrieved", kind)
		}
		verified[kind] = found
		partial[kind] = result.Partial
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"unchanged_full_snapshot_verified": unchangedVerified, "source": spec.Key, "coverage": spec.Coverage, "stable_url": spec.URL, "mode": map[bool]string{true: "live-retrieval-parser-publication-search-enrich", false: "existing-live-artifact-parser-publication-search-enrich"}[*fetch], "artifact_name": filepath.Base(path), "artifact_bytes": size, "artifact_sha256": hex.EncodeToString(hash.Sum(nil)), "parsed": parsed, "generation": publication.Generation, "entity_type_search_enrich_verified": verified, "search_partial_by_type": partial, "duration_ms": time.Since(started).Milliseconds(), "rights_state": "unreviewed", "tenant_grants_created": 0, "production_activated": false, "database_scope": "new disposable verification database, dropped at exit", "matching_quality_claim": false})
}
