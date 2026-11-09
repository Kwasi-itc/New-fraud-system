package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/mysqlruntime"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/localdev"
)

func run() error {
	if _, err := localdev.Origin(os.Getenv("LOCAL_MYSQL_NO_AUTH"), os.Getenv("APP_ORIGIN")); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, 20*time.Second)
	owner, err := mysqlruntime.NewLocalCreation(startup)
	cancel()
	if err != nil {
		return err
	}
	defer owner.Target.Close()
	defer owner.Reference.Close()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	logger.Info("starting MySQL index worker", "concurrency", 1, "lease", "1m", "maintenance_interval", "5s")
	server := &http.Server{Addr: ":8082", ReadHeaderTimeout: 2 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/readyz" {
			http.NotFound(w, r)
			return
		}
		check, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if ctx.Err() != nil || owner.Target.PingContext(check) != nil {
			http.Error(w, "worker unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ready","database":"mysql","worker":"index_jobs"}`))
	})}
	serverErrors := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
			stop()
		}
	}()
	err = owner.RunWorker(ctx, logger)
	shutdown, closeServer := context.WithTimeout(context.Background(), 5*time.Second)
	defer closeServer()
	shutdownErr := server.Shutdown(shutdown)
	select {
	case hostErr := <-serverErrors:
		return errors.Join(err, hostErr, shutdownErr)
	default:
	}
	if ctx.Err() != nil && errors.Is(err, context.Canceled) {
		return shutdownErr
	}
	return errors.Join(err, shutdownErr)
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--healthcheck" {
		client := &http.Client{Timeout: 3 * time.Second}
		response, err := client.Get("http://127.0.0.1:8082/readyz")
		if err != nil {
			os.Exit(1)
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			os.Exit(1)
		}
		return
	}
	if len(os.Args) != 1 {
		slog.Error("unexpected worker arguments")
		os.Exit(1)
	}
	if err := run(); err != nil {
		slog.Error("MySQL worker stopped", "error", err)
		os.Exit(1)
	}
}
