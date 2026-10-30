package main

import (
	"context"
	"flag"
	"fmt"
	R "github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/mysqlruntime"
	"log/slog"
	"net"
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
	listen := flag.String("listen", "127.0.0.1:0", "isolated listen address")
	flag.Parse()
	if *config == "" || *ready == "" {
		return fmt.Errorf("config and ready-file required")
	}
	cfg, err := R.Load(*config)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	service, err := R.Open(startup, cfg, false)
	if err != nil {
		return err
	}
	defer service.DB.Close()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	return service.Serve(ctx, listener, logger, func() error { return R.PublishReady(*ready, listener.Addr().String()) })
}
