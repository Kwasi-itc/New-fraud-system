package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	R "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqlruntime"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/google/uuid"
	"strings"
)

func run() error {
	worker := flag.Bool("worker", false, "run local scheduled/async workers with private health HTTP")
	health := flag.Bool("healthcheck", false, "check local HTTP readiness")
	routes := flag.Bool("routes", false, "print actual local API route inventory for acceptance checks")
	reviewFixture := flag.String("review-fixture-decision", "", "create a local case linked to this existing decision for acceptance checks")
	receipts := flag.String("delivery-receipts", "", "inspect bounded local delivery receipts: kind/resource-uuid")
	tenantFlag := flag.String("tenant", "", "tenant for operator receipt/review diagnostics; defaults to the fixture tenant")
	flag.Parse()
	if *health {
		client := &http.Client{Timeout: 7 * time.Second}
		response, err := client.Get("http://127.0.0.1:8082/readyz")
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			return errors.New("decision runtime not ready")
		}
		return nil
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	app, db, err := R.OpenLocal(startup, *worker)
	if err != nil {
		return err
	}
	defer db.Close()
	if *receipts != "" {
		kind, resource, ok := strings.Cut(*receipts, "/")
		if !ok {
			return errors.New("kind/resource required")
		}
		id, err := uuid.Parse(resource)
		if err != nil {
			return err
		}
		var items any
		if *tenantFlag != "" {
			t, e := uuid.Parse(*tenantFlag)
			if e != nil {
				return e
			}
			items, err = app.LocalDeliveryReceiptsForTenant(startup, t, ports.JobKind(kind), id)
		} else {
			items, err = app.LocalDeliveryReceipts(startup, ports.JobKind(kind), id)
		}
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"deliveries": items})
	}
	if *reviewFixture != "" {
		id, err := uuid.Parse(*reviewFixture)
		if err != nil || id == uuid.Nil {
			return errors.New("valid decision required")
		}
		var caseID uuid.UUID
		if *tenantFlag != "" {
			t, e := uuid.Parse(*tenantFlag)
			if e != nil {
				return e
			}
			caseID, err = app.CreateLocalReviewFixtureForTenant(startup, t, id)
		} else {
			caseID, err = app.CreateLocalReviewFixture(startup, id)
		}
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"case_id": caseID})
	}
	if *routes {
		return json.NewEncoder(os.Stdout).Encode(app.RouteInventory())
	}
	server, err := app.HTTPServer("0.0.0.0:8082")
	if err != nil {
		return err
	}
	server.Handler, err = app.LocalHandler()
	if err != nil {
		return err
	}
	finished := make(chan error, 2)
	if *worker {
		go func() {
			finished <- app.RunWorkers(ctx, func(_ context.Context, err error) error {
				slog.Error("local decision worker error", "error", err)
				return nil
			})
		}()
	}
	go func() { finished <- server.ListenAndServe() }()
	select {
	case err = <-finished:
		stop()
	case <-ctx.Done():
	}
	shutdown, close := context.WithTimeout(context.Background(), 20*time.Second)
	defer close()
	serverErr := server.Shutdown(shutdown)
	workerErr := app.Shutdown(shutdown)
	if errors.Is(err, http.ErrServerClosed) || errors.Is(err, context.Canceled) {
		err = nil
	}
	return errors.Join(err, serverErr, workerErr)
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
