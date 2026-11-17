// source-import is a private operator CLI for controlled records only. It never
// reads the application's default DATABASE_URL or fetches an external URL.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceconfig"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceingest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Controlled source import failed:", err)
		os.Exit(1)
	}
}
func run() error {
	file := flag.String("file", "", "Controlled JSON fixture path")
	dataset := flag.String("dataset", "", "Dataset ID")
	owner := flag.String("owner-tenant", "", "Tenant UUID; empty means global controlled data")
	label := flag.String("label", "", "Dataset label for explicit registration")
	grants := flag.String("grant-tenants", "", "Tenant UUIDs granted access during registration")
	register := flag.Bool("register", false, "Explicitly register a new controlled dataset")
	stageOnly := flag.Bool("stage-only", false, "Validate/stage without publication")
	publish := flag.String("publish-generation", "", "Publish a previously staged generation")
	rollback := flag.String("rollback-generation", "", "Restore a retained publication as a new fenced publication")
	recover := flag.Bool("recover-interrupted", false, "Fail expired staging leases; never change active data")
	cleanup := flag.Bool("cleanup-unpublished", false, "Clean one bounded expired unpublished page under an explicit retention policy")
	artifacts := flag.Bool("cleanup-artifacts", false, "Clean one bounded page of explicitly expired unreferenced failed/rejected artifacts")
	failedDays := flag.Int("retain-failed-days", 0, "Set explicit unpublished retention policy (minimum 1 day)")
	stagedDays := flag.Int("retain-staged-days", 0, "Set explicit staged retention policy (minimum 1 day)")
	legalHold := flag.Bool("legal-hold", true, "Preserve unpublished data regardless of age; published evidence is always retained")
	flag.Parse()
	dsn, secretErr := sourceconfig.ReadSecret("SOURCE_DATABASE_URL")
	if secretErr != nil || dsn == "" {
		return fmt.Errorf("SOURCE_DATABASE_URL is required; no default database is selected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	config, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		return fmt.Errorf("invalid explicit source database configuration")
	}
	config.MaxConns = 2
	config.ConnConfig.RuntimeParams["statement_timeout"] = "30000"
	config.ConnConfig.RuntimeParams["lock_timeout"] = "5000"
	db, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return fmt.Errorf("source database configuration invalid")
	}
	defer db.Close()
	key := sourceingest.Key{Dataset: *dataset}
	if *owner != "" {
		key.Owner, err = uuid.Parse(*owner)
		if err != nil || key.Owner == uuid.Nil {
			return fmt.Errorf("invalid owner UUID")
		}
	}
	store := sourceingest.Store{DB: db}
	if *artifacts {
		n, e := store.CleanupArtifacts(ctx)
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"expired_unreferenced_artifacts": n})
	}
	if *failedDays != 0 || *stagedDays != 0 {
		if *failedDays < 1 || *stagedDays < 1 || *failedDays > 36500 || *stagedDays > 36500 {
			return fmt.Errorf("retention days must be between 1 and 36500")
		}
		_, err = db.Exec(ctx, `INSERT INTO screening.source_retention_policies(owner_scope,dataset_id,failed_after,staged_after,legal_hold) VALUES($1,$2,make_interval(days=>$3),make_interval(days=>$4),$5) ON CONFLICT(owner_scope,dataset_id) DO UPDATE SET failed_after=excluded.failed_after,staged_after=excluded.staged_after,legal_hold=excluded.legal_hold`, key.Owner, key.Dataset, *failedDays, *stagedDays, *legalHold)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "retention-policy-saved", "legal_hold": *legalHold, "published_retention": "indefinite"})
	}
	if *recover {
		n, e := store.RecoverInterrupted(ctx, 100)
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"expired_staging_recovered": n})
	}
	if *cleanup {
		n, e := store.CleanupUnpublished(ctx)
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"unpublished_records_cleaned": n})
	}
	if *rollback != "" {
		retained, e := uuid.Parse(*rollback)
		if e != nil || retained == uuid.Nil {
			return fmt.Errorf("invalid rollback generation")
		}
		result, e := store.Rollback(ctx, key, retained)
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(result)
	}
	if *register {
		tenants := []uuid.UUID{}
		for _, value := range strings.Split(*grants, ",") {
			if value == "" {
				continue
			}
			id, e := uuid.Parse(strings.TrimSpace(value))
			if e != nil {
				return fmt.Errorf("invalid grant UUID")
			}
			tenants = append(tenants, id)
		}
		if err = store.Register(ctx, key, *label, tenants); err != nil {
			return err
		}
	}
	var generation uuid.UUID
	if *publish != "" {
		generation, err = uuid.Parse(*publish)
		if err != nil {
			return fmt.Errorf("invalid generation UUID")
		}
	} else {
		input, e := os.Open(*file)
		if e != nil {
			return fmt.Errorf("controlled fixture unavailable")
		}
		defer input.Close()
		generation, err = store.Stage(ctx, key, input)
		if err != nil {
			return err
		}
	}
	if *stageOnly {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "staged", "generation": generation})
	}
	result, err := store.Publish(ctx, key, generation)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
