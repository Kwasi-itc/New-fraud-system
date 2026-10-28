// recover-delivery is a privileged, exact-job legacy recovery command, not an API.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/app"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/reconcile"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/riverjobs"
	storepostgres "github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/postgres"
	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

func main() {
	tenant := flag.String("tenant", "", "exact tenant UUID")
	job := flag.String("job", "", "exact legacy repair job UUID")
	apply := flag.Bool("apply", false, "enqueue missing delivery and audit atomically; default inspects only")
	actor := flag.String("actor", "", "operator identity for recovery audit")
	reason := flag.String("reason", "", "recovery ticket/reason")
	flag.Parse()
	if flag.NArg() != 0 {
		log.Fatal("unexpected positional arguments")
	}
	tenantID, err := uuid.Parse(*tenant)
	if err != nil || tenantID == uuid.Nil {
		log.Fatal("valid tenant UUID required")
	}
	jobID, err := uuid.Parse(*job)
	if err != nil || jobID == uuid.Nil {
		log.Fatal("valid job UUID required")
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := storepostgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	client, err := river.NewClient(riverpgxv5.New(db), &river.Config{})
	if err != nil {
		log.Fatal(err)
	}
	producer := riverjobs.NewRiverIndexJobEnqueuer(client, cfg.IndexWorkerMaxAttempts, cfg.IndexJobQueueName)
	recovery, err := reconcile.NewDeliveryRecovery(db, producer)
	if err != nil {
		log.Fatal(err)
	}
	result, err := recovery.Run(ctx, reconcile.DeliveryRecoveryRequest{TenantID: tenantID, JobID: jobID, Apply: *apply, Actor: *actor, Reason: *reason})
	if err != nil {
		log.Fatal(err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		log.Fatal(err)
	}
	if *apply && !result.Recovered {
		os.Exit(2)
	}
}
