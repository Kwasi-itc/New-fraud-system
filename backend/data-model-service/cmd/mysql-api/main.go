package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/mysqlruntime"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/localdev"
	"github.com/go-sql-driver/mysql"
)

func run() error {
	if _, err := localdev.Origin(os.Getenv("LOCAL_MYSQL_NO_AUTH"), os.Getenv("APP_ORIGIN")); err != nil {
		return err
	}
	secret, err := os.ReadFile("/run/secrets/data_model_mysql_password")
	if err != nil {
		return err
	}
	cfg := mysql.NewConfig()
	cfg.User, cfg.Passwd, cfg.Net, cfg.Addr, cfg.DBName = "local_dm_api", string(secret), "tcp", "mysql:3306", "fraud"
	cfg.ParseTime = true
	cfg.Timeout, cfg.ReadTimeout, cfg.WriteTimeout = 5*time.Second, 15*time.Second, 15*time.Second
	cfg.Params = map[string]string{"time_zone": "'+00:00'", "transaction_isolation": "'READ-COMMITTED'"}
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		return err
	}
	db := sql.OpenDB(connector)
	defer db.Close()
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(5 * time.Minute)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, 10*time.Second)
	err = db.PingContext(startup)
	cancel()
	if err != nil {
		return err
	}
	handler, err := mysqlruntime.NewLocalHTTP(db, os.Getenv("LOCAL_MYSQL_NO_AUTH"), os.Getenv("APP_ORIGIN"))
	if err != nil {
		return err
	}
	server := &http.Server{Addr: ":8080", Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 75 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	log.Print("local MySQL API listening on :8080; login disabled; docs at /docs")
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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
