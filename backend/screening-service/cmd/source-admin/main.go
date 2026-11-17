// source-admin is private DB-role authenticated administration. It never accepts
// tenant/customer URLs, reads a dotenv file or changes production credentials.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceconfig"
	"os"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcecollector"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceconnectors"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceingest"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := run(); err != nil {
		if errors.Is(err, errOperationalAlert) {
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, "source administration failed:", err)
		os.Exit(1)
	}
}

var errOperationalAlert = errors.New("operational alert")

func run() error {
	action := flag.String("action", "catalog", "catalog, register, status, diagnostics, health, enable, approve-use, pause or retry")
	maxQueueAge := flag.Duration("max-queue-age", 5*time.Minute, "Operational alert threshold, 30s..24h; approve deployment policy before scheduling")
	source := flag.String("source", "", "Explicit registry source key")
	dataset := flag.String("dataset", "", "Dataset ID")
	rights := flag.String("rights-review-reference", "", "Approved source-specific rights/retention review reference; required for enable")
	poll := flag.Duration("poll-interval", 24*time.Hour, "UTC check cadence, not a publisher SLA")
	stale := flag.Duration("max-stale", 0, "Explicit permitted age after successful source verification; required for enable")
	flag.Parse()
	if *action == "catalog" {
		return json.NewEncoder(os.Stdout).Encode(sourceconnectors.Registry())
	}
	dsn, secretErr := sourceconfig.ReadSecret("SOURCE_DATABASE_URL")
	if secretErr != nil {
		return secretErr
	}
	if dsn == "" {
		return fmt.Errorf("SOURCE_DATABASE_URL is required; no default is selected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	config, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		return fmt.Errorf("source database configuration invalid")
	}
	config.MaxConns = 2
	config.ConnConfig.RuntimeParams["statement_timeout"] = "30000"
	config.ConnConfig.RuntimeParams["lock_timeout"] = "5000"
	db, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return fmt.Errorf("source database unavailable")
	}
	defer db.Close()
	key := sourceingest.Key{Dataset: *dataset}
	switch *action {
	case "health":
		health, e := (sourcecollector.Collector{DB: db}).Health(ctx, *maxQueueAge)
		if e != nil {
			return e
		}
		if e = json.NewEncoder(os.Stdout).Encode(health); e != nil {
			return e
		}
		if health.Status == "degraded" {
			return errOperationalAlert
		}
		return nil
	case "register":
		spec, e := sourceconnectors.Lookup(*source)
		if e != nil {
			return e
		}
		if e = (sourcecollector.Pipeline{DB: db}).Register(ctx, key, spec); e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"dataset": *dataset, "enabled": false, "rights_state": "unreviewed"})
	case "diagnostics":
		raw, e := (sourcecollector.Collector{DB: db}).Diagnostics(ctx)
		if e != nil {
			return e
		}
		_, e = os.Stdout.Write(append(raw, '\n'))
		return e
	case "status":
		var raw []byte
		err = db.QueryRow(ctx, `SELECT jsonb_build_object('dataset',c.dataset_id,'source',c.connector,'enabled',c.enabled,'rights_state',c.rights_state,'last_attempt_at',c.last_attempt_at,'last_success_at',c.last_success_at,'next_check_at',c.next_check_at,'error_code',c.last_error,'failures',c.failures,'ready',r.ready,'active_generation',d.active_generation) FROM screening.source_connections c JOIN screening.source_datasets d ON (d.owner_scope,d.id)=(c.owner_scope,c.dataset_id) JOIN screening.source_readiness_v1 r ON(r.owner_scope,r.dataset_id)=(c.owner_scope,c.dataset_id) WHERE c.owner_scope=$1 AND c.dataset_id=$2`, key.Owner, key.Dataset).Scan(&raw)
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(append(raw, '\n'))
		return err
	case "enable", "pause", "retry", "approve-use":
		if ((*action == "enable" || *action == "approve-use") && (*rights == "" || len(*rights) > 500 || *stale < 15*time.Minute || *stale > 365*24*time.Hour)) || *poll < 15*time.Minute || *poll > 365*24*time.Hour {
			return fmt.Errorf("explicit reviewed rights and bounded cadence/freshness policy are required")
		}
		tx, e := db.Begin(ctx)
		if e != nil {
			return e
		}
		defer tx.Rollback(ctx)
		var connector string
		if err = tx.QueryRow(ctx, `SELECT connector FROM screening.source_connections WHERE owner_scope=$1 AND dataset_id=$2 FOR UPDATE`, key.Owner, key.Dataset).Scan(&connector); err != nil {
			return fmt.Errorf("source connection not found")
		}
		if *action == "approve-use" {
			spec, e := sourceconnectors.NoticeSource(connector)
			if e != nil || !spec.Intake {
				return fmt.Errorf("approve-use requires a reviewed-notice source")
			}
		}
		if *action == "enable" || *action == "retry" {
			if _, e := sourceconnectors.Lookup(connector); e != nil {
				return fmt.Errorf("automatic collection is unavailable for this source; reviewed notices require approve-use")
			}
		}
		var tag pgconn.CommandTag
		if *action == "approve-use" {
			tag, err = tx.Exec(ctx, `UPDATE screening.source_connections SET enabled=false,rights_state='approved',rights_review_reference=$3,max_stale=make_interval(secs=>$4),lease_token=NULL,lease_until=NULL,fence=fence+1 WHERE owner_scope=$1 AND dataset_id=$2`, key.Owner, key.Dataset, *rights, int((*stale).Seconds()))
		} else if *action == "enable" {
			tag, err = tx.Exec(ctx, `UPDATE screening.source_connections SET enabled=true,rights_state='approved',rights_review_reference=$3,poll_interval=make_interval(secs=>$4),max_stale=make_interval(secs=>$5),next_check_at=now(),failures=0 WHERE owner_scope=$1 AND dataset_id=$2`, key.Owner, key.Dataset, *rights, int((*poll).Seconds()), int((*stale).Seconds()))
		} else if *action == "pause" {
			tag, err = tx.Exec(ctx, `UPDATE screening.source_connections SET enabled=false,lease_token=NULL,lease_until=NULL,fence=fence+1 WHERE owner_scope=$1 AND dataset_id=$2`, key.Owner, key.Dataset)
		} else {
			tag, err = tx.Exec(ctx, `UPDATE screening.source_connections SET enabled=true,next_check_at=now(),failures=0,lease_token=NULL,lease_until=NULL,fence=fence+1 WHERE owner_scope=$1 AND dataset_id=$2 AND rights_state='approved' AND rights_review_reference<>''`, key.Owner, key.Dataset)
		}
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("source is missing or its rights review does not allow this operation")
		}
		detail, _ := json.Marshal(map[string]any{"action": *action, "rights_review_reference": *rights})
		if _, err = tx.Exec(ctx, `INSERT INTO screening.source_lifecycle_audit(owner_scope,dataset_id,action,actor,detail) VALUES($1,$2,'source-configuration',current_user,$3)`, key.Owner, key.Dataset, detail); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	return fmt.Errorf("unsupported source operation")
}
