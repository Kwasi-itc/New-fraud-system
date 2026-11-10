package main

import (
	"context"
	"errors"
	"log"
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
	worker, err := mysqlruntime.NewLocalCreation(startup)
	cancel()
	if err != nil {
		return err
	}
	defer worker.Target.Close()
	defer worker.Reference.Close()
	if len(os.Args) == 2 && os.Args[1] == "--sync-local-consumers" {
		bounded, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		return worker.SyncLocalConsumers(bounded)
	}
	handler, err := mysqlruntime.NewLocalAuthorHTTP(worker)
	if err != nil {
		return err
	}
	server := &http.Server{Addr: ":8081", Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 75 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 65*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			return err
		}
		err := <-done
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
