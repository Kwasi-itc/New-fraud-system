package main

import (
	"context"
	"flag"
	"fmt"
	R "github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/mysqlruntime"
	"log/slog"
	"net"
	"net/http"
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
	local := flag.Bool("local", false, "explicit local Docker runtime")
	health := flag.Bool("healthcheck", false, "check local HTTP readiness")
	flag.Parse()
	if *health {
		client := &http.Client{Timeout: 7 * time.Second}
		response, err := client.Get("http://127.0.0.1:8081/readyz")
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("API is not ready")
		}
		return nil
	}
	if !*local && (*config == "" || *ready == "") {
		return fmt.Errorf("config and ready-file required")
	}
	var cfg R.Config
	var err error
	if *local {
		cfg, err = R.LoadLocal(false)
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
	return service.Serve(ctx, listener, logger, func() error {
		if *local {
			return nil
		}
		return R.PublishReady(*ready, listener.Addr().String())
	})
}
