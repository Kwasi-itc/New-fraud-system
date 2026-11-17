// source-worker is separately deployed collection, never started by the API.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceconfig"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcecollector"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceconnectors"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceingest"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "source worker failed:", err)
		os.Exit(1)
	}
}
func run() error {
	mode := flag.String("mode", "once", "once or poll; only explicitly enabled/reviewed sources run")
	interval := flag.Duration("poll-interval", 30*time.Second, "UTC scheduler poll interval")
	batch := flag.Int("batch", 4, "Maximum sources checked per cycle, 1..20")
	flag.Parse()
	if (*mode != "once" && *mode != "poll") || *interval < 5*time.Second || *interval > time.Hour || *batch < 1 || *batch > 20 {
		return fmt.Errorf("invalid source worker bounds")
	}
	dsn, secretErr := sourceconfig.ReadSecret("SOURCE_DATABASE_URL")
	if secretErr != nil {
		return secretErr
	}
	if dsn == "" {
		return fmt.Errorf("SOURCE_DATABASE_URL is required; application defaults are not read")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("invalid source database configuration")
	}
	cfg.MaxConns = 4
	cfg.MinConns = 0
	cfg.ConnConfig.RuntimeParams["application_name"] = "screening-source-worker"
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "30000"
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = "5000"
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("source database unavailable")
	}
	defer db.Close()
	dir, err := os.MkdirTemp("", "screening-source-worker-")
	if err != nil {
		return fmt.Errorf("private artifact directory unavailable")
	}
	// Every downloaded path is generated in this private directory and removed by
	// its attempt. Only the empty owned directory is removed at orderly shutdown.
	defer os.Remove(dir)
	collector := sourcecollector.Collector{DB: db, Fetcher: sourceconnectors.NewFetcher(), Dir: dir}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	for {
		started := time.Now()
		attempts := 0
		if _, err = collector.Recover(ctx, 100); err == nil {
			_, err = (sourceingest.Store{DB: db, GlobalOnly: true}).RecoverInterrupted(ctx, 100)
		}
		if err == nil {
			for i := 0; i < *batch; i++ {
				worked, e := collector.RunOne(ctx)
				if e != nil {
					err = e
					break
				}
				if !worked {
					break
				}
				attempts++
			}
		}
		if err != nil {
			logger.Error("source cycle retained for recovery", "code", "source_cycle_failed", "attempts", attempts)
		} else {
			if _, e := (sourceingest.Store{DB: db, GlobalOnly: true}).CleanupUnpublished(ctx); e != nil {
				logger.Error("source cleanup retained", "code", "source_cleanup_failed")
			}
			if _, e := (sourceingest.Store{DB: db, GlobalOnly: true}).CleanupArtifacts(ctx); e != nil {
				logger.Error("artifact cleanup retained", "code", "artifact_cleanup_failed")
			}
			var memory runtime.MemStats
			runtime.ReadMemStats(&memory)
			pool := db.Stat()
			json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "source_cycle", "attempts": attempts, "duration_ms": time.Since(started).Milliseconds(), "heap_inuse_bytes": memory.HeapInuse, "pool_max": pool.MaxConns(), "pool_acquired": pool.AcquiredConns(), "pool_wait_micros": pool.EmptyAcquireWaitTime().Microseconds(), "note": "Attempt receipts carry published/unchanged/failed/quarantined outcomes; no eligible source is enabled automatically"})
		}
		if *mode == "once" {
			if err != nil {
				return fmt.Errorf("source cycle failed; inspect persisted attempt diagnostics")
			}
			return nil
		}
		timer := time.NewTimer(*interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}
