package main

import (
	"context"
	"flag"
	"fmt"
	R "github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/mysqlruntime"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	config := flag.String("config", "", "isolated runtime config file")
	ready := flag.String("ready-file", "", "exclusive process readiness file")
	local := flag.Bool("local", false, "explicit local Docker runtime")
	health := flag.Bool("healthcheck", false, "check local worker database prerequisites")
	flag.Parse()
	if !*local && (*config == "" || *ready == "") {
		return fmt.Errorf("config and ready-file required")
	}
	var cfg R.Config
	var err error
	if *local {
		cfg, err = R.LoadLocal(true)
	} else {
		cfg, err = R.Load(*config)
	}
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	service, err := R.Open(startup, cfg, true)
	if err != nil {
		return err
	}
	defer service.DB.Close()
	if *health {
		return nil
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	return service.Worker(ctx, logger, func() error {
		if *local {
			return nil
		}
		return R.PublishReady(*ready, "ready")
	})
}
