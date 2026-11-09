package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/internal/mysqlauth"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, "auth MySQL process failed:", e)
		os.Exit(1)
	}
}
func run() error {
	worker := flag.Bool("worker", false, "run invitation delivery and expired login retention")
	health := flag.Bool("healthcheck", false, "check this role's readiness")
	config := flag.String("config", "/runtime/config.json", "private runtime configuration")
	secrets := flag.String("secrets-dir", "/run/secrets", "private credentials directory")
	listen := flag.String("listen", "", "HTTP listen address; worker health is private")
	flag.Parse()
	if *listen == "" {
		*listen = ":8087"
		if *worker {
			*listen = ":8097"
		}
	}
	if *health {
		_, port, e := net.SplitHostPort(*listen)
		if e != nil {
			return e
		}
		client := http.Client{Timeout: 5 * time.Second}
		r, e := client.Get("http://127.0.0.1:" + port + "/readyz")
		if e != nil {
			return e
		}
		defer r.Body.Close()
		if r.StatusCode != http.StatusNoContent {
			return errors.New("auth role not ready")
		}
		return nil
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	c, e := mysqlauth.LoadConfig(*config)
	if e != nil {
		return e
	}
	s, inv, e := mysqlauth.OpenRuntime(startup, c, *secrets, *worker)
	if e != nil {
		return e
	}
	defer s.DB.Close()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	var handler http.Handler
	workerDone := make(chan error, 1)
	if *worker {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
			check, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if s.Ready(check) != nil {
				http.Error(w, "not ready", 503)
				return
			}
			w.WriteHeader(204)
		})
		handler = mux
		go func() { workerDone <- inv.Run(ctx, c.SMTP, logger) }()
	} else {
		handler, e = mysqlauth.API(startup, c, *secrets, s, inv, logger)
		if e != nil {
			return e
		}
	}
	server := http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second}
	httpDone := make(chan error, 1)
	go func() { httpDone <- server.ListenAndServe() }()
	logger.Info("auth MySQL role started", "worker", *worker, "listen", *listen)
	joined := false
	select {
	case e = <-httpDone:
		if !errors.Is(e, http.ErrServerClosed) {
			stop()
		}
	case e = <-workerDone:
		joined = true
		stop()
	case <-ctx.Done():
	}
	stop()
	shutdown, closeCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer closeCancel()
	closeErr := server.Shutdown(shutdown)
	if *worker && !joined {
		select {
		case workerErr := <-workerDone:
			e = errors.Join(e, workerErr)
		case <-shutdown.Done():
			return errors.New("auth worker shutdown timeout")
		}
	}
	if errors.Is(e, http.ErrServerClosed) {
		e = nil
	}
	return errors.Join(e, closeErr)
}
