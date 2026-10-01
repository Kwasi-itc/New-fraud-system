// outbox is an operator tool using the database role's privileges, never a user
// authentication substitute. It exposes delivery metadata, not evidence payloads.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	store "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/store/postgres"
	"github.com/google/uuid"
)

func run() error {
	tenantArg := flag.String("tenant", "", "required tenant UUID")
	replayArg := flag.String("replay", "", "failed/delivered event UUID to replay with its original identity")
	statusArg := flag.String("status", "failed", "delivery status: failed, pending, delivering or delivered")
	flag.Parse()
	switch *statusArg {
	case "failed", "pending", "delivering", "delivered":
	default:
		return fmt.Errorf("invalid -status")
	}
	tenant, err := uuid.Parse(*tenantArg)
	if err != nil || tenant == uuid.Nil {
		return fmt.Errorf("valid -tenant required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if os.Getenv("DATABASE_URL") == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	db, err := store.NewPool(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer db.Close()
	if *replayArg != "" {
		id, err := uuid.Parse(*replayArg)
		if err != nil || id == uuid.Nil {
			return fmt.Errorf("valid -replay event ID required")
		}
		if err := (store.Maintenance{DB: db}).Replay(ctx, tenant, id); err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"event_id": id, "status": "pending"})
	}
	rows, err := db.Query(ctx, `SELECT jsonb_build_object('id',id,'event_type',event_type,'status',status,'attempts',attempts,'next_attempt_at',next_attempt_at,'lease_until',lease_until,'last_error',last_error,'created_at',created_at) FROM case_manager.outbox_events WHERE tenant_id=$1 AND status=$2 ORDER BY created_at,id LIMIT 100`, tenant, *statusArg)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var data json.RawMessage
		if err := rows.Scan(&data); err != nil {
			return err
		}
		if err := json.NewEncoder(os.Stdout).Encode(data); err != nil {
			return err
		}
	}
	return rows.Err()
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
