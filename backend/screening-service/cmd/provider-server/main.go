package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/internalprovider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/providerproduct"
)

func main() {
	health := flag.Bool("healthcheck", false, "Probe this provider's readiness without opening database credentials")
	flag.Parse()
	if *health {
		port := os.Getenv("PROVIDER_PORT")
		if port == "" {
			port = "8091"
		}
		response, e := (&http.Client{Timeout: 3 * time.Second}).Get("http://127.0.0.1:" + port + "/readyz")
		if e != nil {
			os.Exit(1)
		}
		response.Body.Close()
		if response.StatusCode != 200 {
			os.Exit(1)
		}
		return
	}
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, "provider failed:", e)
		os.Exit(1)
	}
}
func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, e := providerproduct.OpenDatabase(ctx)
	if e != nil {
		return e
	}
	defer db.Close()
	if _, e = providerproduct.Topology(ctx, db); e != nil {
		return e
	}
	address, e := providerproduct.ListenAddress()
	if e != nil {
		return e
	}
	timeout := 2 * time.Second
	if raw := os.Getenv("PROVIDER_REQUEST_TIMEOUT"); raw != "" {
		timeout, e = time.ParseDuration(raw)
		if e != nil {
			return errors.New("invalid provider timeout")
		}
	}
	max := 16
	if raw := os.Getenv("PROVIDER_MAX_CONCURRENT"); raw != "" {
		max, e = strconv.Atoi(raw)
		if e != nil {
			return errors.New("invalid provider concurrency")
		}
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	handler, e := (providerproduct.Server{DB: db, Provider: internalprovider.Provider{DB: db}, Access: providerproduct.Credentials{DB: db}, Logger: logger, Timeout: timeout, MaxConcurrent: max}).Handler()
	if e != nil {
		return e
	}
	server := &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	logger.Info("standalone provider listening", "address", address)
	select {
	case e = <-done:
		if errors.Is(e, http.ErrServerClosed) {
			return nil
		}
		return e
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}
