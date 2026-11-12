package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/ec2test"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/httpapi"
	R "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/mysqlruntime"
	store "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, "case MySQL runtime failed:", e)
		os.Exit(1)
	}
}
func run() error {
	worker := flag.Bool("worker", false, "run native case maintenance and delivery")
	health := flag.Bool("healthcheck", false, "check local readiness")
	replay := flag.String("replay", "", "replay one terminal notification UUID using worker credentials")
	tenant := flag.String("tenant", "", "explicit tenant UUID required with --replay")
	inboxOutbox := flag.Bool("inbox-outbox", false, "replay an inbox notification instead of a case notification")
	flag.Parse()
	if *replay == "" && (*tenant != "" || *inboxOutbox) {
		return errors.New("tenant/outbox selection requires --replay")
	}
	if *health {
		client := http.Client{Timeout: 4 * time.Second}
		resp, e := client.Get("http://127.0.0.1:8086/readyz")
		if e != nil {
			return e
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return errors.New("case runtime not ready")
		}
		return nil
	}
	if !R.LocalEnabled() {
		return errors.New("CASE_LOCAL_MYSQL=1 required for local runtime")
	}
	c, e := R.Load("/runtime/config.json")
	if e != nil {
		return e
	}
	role := "api"
	if *worker {
		role = "worker"
	}
	password, e := R.Secret("case_" + role + "_password")
	if e != nil {
		return e
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, e := ec2test.Open(ctx, mysqlstore.Config{Address: c.Address, Database: c.Database, User: "local_case_" + role, Password: password, AllowInsecureTransport: true, MaxOpen: 8, MaxIdle: 4, ConnectTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 20 * time.Second, ConnMaxLifetime: 5 * time.Minute, ConnMaxIdleTime: time.Minute, LockWaitTimeout: 5 * time.Second})
	if e != nil {
		return e
	}
	defer db.Close()
	if *replay != "" {
		if !*worker {
			return errors.New("--worker credentials required for notification replay")
		}
		t, e := uuid.Parse(*tenant)
		if e != nil || t == uuid.Nil {
			return errors.New("valid explicit replay tenant required")
		}
		event, e := uuid.Parse(*replay)
		if e != nil || event == uuid.Nil {
			return errors.New("valid replay event UUID required")
		}
		return (store.Maintenance{DB: db, Database: c.Database}).Replay(ctx, t, event, *inboxOutbox)
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	var handler http.Handler
	var last atomic.Int64
	done := make(chan struct{})
	if *worker {
		publisherToken, e := R.Secret("case_publisher_token")
		if e != nil {
			return e
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
			check, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if last.Load() == 0 || time.Since(time.Unix(0, last.Load())) > time.Minute || db.PingContext(check) != nil {
				w.WriteHeader(503)
				return
			}
			w.WriteHeader(200)
		})
		handler = mux
		go func() {
			defer close(done)
			R.RunWorker(ctx, log, store.Maintenance{DB: db, Database: c.Database}, c.PublisherURL, publisherToken, func() { last.Store(time.Now().UnixNano()) })
		}()
	} else {
		close(done)
		key, e := os.ReadFile("/runtime/auth-public.pem")
		if e != nil {
			return e
		}
		verifier, e := access.NewVerifier(map[string][]byte{c.KeyID: key}, c.Issuer, c.Audience)
		if e != nil {
			return e
		}
		token, e := R.Secret("case_service_token")
		if e != nil {
			return e
		}
		cfg := httpapi.RouterConfig{UserVerifier: verifier, ServiceTenantIDs: c.ServiceTenants, AuthMode: "token", AuthToken: token, EnableWorkflowReceipts: true, OutboxPublisherURL: c.PublisherURL}
		if c.ServiceTenantMode == "registered" {
			cfg.ServiceTenantAllowed = func(ctx context.Context, tenant uuid.UUID) (bool, error) {
				var allowed bool
				e := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM fraud.identity_tenants WHERE id=? AND active)", mysqlstore.UUID(tenant)).Scan(&allowed)
				return allowed, e
			}
		}
		router, e := httpapi.NewMySQLRouter(log, db, c.Database, cfg)
		if e != nil {
			return e
		}
		httpapi.MountMySQLDocs(router)
		handler = router
	}
	server := &http.Server{Addr: ":8086", Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: time.Minute}
	errch := make(chan error, 1)
	go func() { errch <- server.ListenAndServe() }()
	log.Info("case MySQL runtime started", "role", role, "port", 8086)
	select {
	case <-ctx.Done():
	case e = <-errch:
		stop()
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	shutdownErr := server.Shutdown(shutdown)
	<-done
	if e != nil && !errors.Is(e, http.ErrServerClosed) {
		return e
	}
	return shutdownErr
}
