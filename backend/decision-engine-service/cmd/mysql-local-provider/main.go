package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	R "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqlruntime"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func run() error {
	health := flag.Bool("healthcheck", false, "check private local provider readiness")
	flag.Parse()
	if *health {
		client := http.Client{Timeout: 5 * time.Second}
		res, err := client.Get("http://127.0.0.1:8090/readyz")
		if err != nil {
			return err
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			return errors.New("local provider not ready")
		}
		return nil
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	provider, db, err := R.OpenLocalProvider(startup)
	cancel()
	if err != nil {
		return err
	}
	defer db.Close()
	server := &http.Server{Addr: "0.0.0.0:8090", Handler: provider.Handler(), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 12 * time.Second, WriteTimeout: 12 * time.Second, IdleTimeout: time.Minute, MaxHeaderBytes: 16 << 10}
	done := make(chan error, 2)
	processorDone := make(chan struct{})
	go func() { defer close(processorDone); done <- provider.Run(ctx) }()
	go func() { done <- server.ListenAndServe() }()
	select {
	case err = <-done:
		stop()
	case <-ctx.Done():
	}
	shutdown, close := context.WithTimeout(context.Background(), 15*time.Second)
	defer close()
	stop()
	closeErr := server.Shutdown(shutdown)
	select {
	case <-processorDone:
	case <-shutdown.Done():
		closeErr = errors.Join(closeErr, shutdown.Err())
	}
	if errors.Is(err, http.ErrServerClosed) || errors.Is(err, context.Canceled) {
		err = nil
	}
	return errors.Join(err, closeErr)
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
