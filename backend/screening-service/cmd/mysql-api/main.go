package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/ec2test"
	caseclient "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/clients/case"
	ingestion "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/clients/ingestion"
	api "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/httpapi"
	R "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/mysqlruntime"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
	store "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/mysql"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, "screening MySQL runtime failed:", e)
		os.Exit(1)
	}
}

type clock struct{}

func (clock) Now() time.Time { return time.Now().UTC() }
func run() error {
	worker := flag.Bool("worker", false, "run native screening workers")
	health := flag.Bool("healthcheck", false, "check readiness")
	replay := flag.String("replay", "", "replay one terminal case callback")
	tenant := flag.String("tenant", "", "explicit callback tenant")
	flag.Parse()
	if *health {
		client := http.Client{Timeout: 4 * time.Second}
		resp, e := client.Get("http://127.0.0.1:8085/readyz")
		if e != nil {
			return e
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return errors.New("screening is not ready")
		}
		return nil
	}
	if os.Getenv("SCREENING_LOCAL_MYSQL") != "1" {
		return errors.New("SCREENING_LOCAL_MYSQL=1 required")
	}
	c, e := R.Load("/runtime/config.json")
	if e != nil {
		return e
	}
	role := "api"
	if *worker {
		role = "worker"
	}
	password, e := R.Secret("screening_" + role + "_password")
	if e != nil {
		return e
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, e := ec2test.Open(ctx, mysqlstore.Config{Address: c.Address, Database: c.Database, User: "local_screening_" + role, Password: password, AllowInsecureTransport: true, MaxOpen: 8, MaxIdle: 4, ConnectTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 20 * time.Second, ConnMaxLifetime: 5 * time.Minute, ConnMaxIdleTime: time.Minute, LockWaitTimeout: 5 * time.Second})
	if e != nil {
		return e
	}
	defer db.Close()
	s, e := store.New(db, c.Database)
	if e != nil {
		return e
	}
	if *replay != "" {
		if !*worker || *tenant == "" {
			return errors.New("worker credentials and explicit tenant required for replay")
		}
		return s.ReplayCallback(ctx, *tenant, *replay)
	}
	if *tenant != "" {
		return errors.New("tenant requires replay")
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	var handler http.Handler
	done := make(chan struct{})
	var last atomic.Int64
	if *worker {
		caseToken, e := R.Secret("case_service_token")
		if e != nil {
			return e
		}
		ingestionToken, e := R.Secret("ingestion_runtime_token")
		if e != nil {
			return e
		}
		deps := api.MySQLDependencies{Provider: R.LocalProvider{}, Inbox: s, Cases: caseclient.NewHTTPClient(c.CaseURL, caseToken, 10*time.Second)}
		svc := api.MySQLService(s, deps)
		delivery := service.NewCaseDeliveryService(s, deps.Cases, log)
		w := R.Worker{Store: s, Dispatch: service.NewDispatchService(s, clock{}, s.Screenings(), s.ScreeningMatches(), deps.Provider, nil), Continuous: service.NewContinuousWorkerService(s, clock{}, s.ContinuousConfigs(), s.MonitoredObjects(), ingestion.NewAuthenticatedHTTPClient(c.IngestionURL, ingestionToken, 10*time.Second), svc), Datasets: service.NewDatasetUpdateWorkerService(s, clock{}, s.DatasetUpdateJobs(), s.ContinuousConfigs(), s.MonitoredObjects(), deps.Provider, store.DomainPollingEnqueuer{}), Delivery: &delivery, Log: log, DB: db, Database: c.Database}
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
		go func() { defer close(done); w.Run(ctx, func() { last.Store(time.Now().UnixNano()) }) }()
	} else {
		close(done)
		key, e := os.ReadFile("/runtime/auth-public.pem")
		if e != nil {
			return e
		}
		verifier, e := identity.NewVerifier(map[string][]byte{c.KeyID: key}, c.Issuer, c.Audience)
		if e != nil {
			return e
		}
		token, e := R.Secret("screening_service_token")
		if e != nil {
			return e
		}
		operator, e := R.Secret("screening_operator_token")
		if e != nil {
			return e
		}
		intake, e := R.Secret("screening_intake_token")
		if e != nil {
			return e
		}
		blob, e := R.NewLocalBlob(db, c.Database, c.Origin)
		if e != nil {
			return e
		}
		router, e := api.NewMySQLRouter(log, db, c.Database, api.RouterConfig{InvestigatorVerifier: verifier, AuthMode: "token", AuthToken: token, ScreeningOperatorToken: operator, ScreeningIntakeToken: intake, ScreeningProviderURL: "local-development", CaseServiceURL: c.CaseURL, IngestionServiceURL: c.IngestionURL}, api.MySQLDependencies{Provider: R.LocalProvider{}, Capabilities: R.LocalCapabilities(), Inbox: s, Blob: blob})
		if e != nil {
			return e
		}
		handler = router
	}
	server := http.Server{Addr: ":8085", Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: time.Minute}
	errch := make(chan error, 1)
	go func() { errch <- server.ListenAndServe() }()
	log.Info("screening MySQL runtime started", "role", role, "port", 8085)
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
