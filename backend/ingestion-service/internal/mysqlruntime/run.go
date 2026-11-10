package mysqlruntime

import (
	"context"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/ec2test"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/httpapi"
	store "github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/localdev"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/work"
)

func Open(ctx context.Context, c Config, worker bool) (*Service, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	password := os.Getenv("INGESTION_RUNTIME_PASSWORD")
	if c.Authority == "local-mysql-development" {
		secret := "ingestion_api_password"
		if worker {
			secret = "ingestion_worker_password"
		}
		var err error
		password, err = LocalSecret(secret)
		if err != nil {
			return nil, err
		}
	}
	if password == "" {
		return nil, errors.New("runtime password required")
	}
	lockWait := (c.Timeout - time.Second).Truncate(time.Second)
	if lockWait < time.Second {
		lockWait = time.Second
	}
	db, err := ec2test.Open(ctx, mysqlstore.Config{Address: c.Address, Database: c.Database, User: c.User, Password: password, AllowInsecureTransport: true, LockWaitTimeout: lockWait, MaxOpen: c.Pool, MaxIdle: c.Pool, ConnectTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 20 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute})
	if err != nil {
		return nil, err
	}
	s := &Service{db, c}
	if err = s.Check(ctx, worker); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Serve joins graceful shutdown before returning and closes no caller-owned pool.
func (s *Service) Serve(ctx context.Context, listener net.Listener, logger *slog.Logger, started func() error) error {
	if err := s.Config.Validate(); err != nil {
		return err
	}
	var origins []string
	if s.Config.Authority == "local-mysql-development" {
		origin, err := localdev.Origin(os.Getenv("LOCAL_MYSQL_NO_AUTH"), os.Getenv("APP_ORIGIN"))
		if err != nil {
			return err
		}
		origins = []string{origin}
	}
	router, err := httpapi.NewWriteWorkRouter(logger, httpapi.RouterConfig{AuthMode: "token", AuthToken: s.Config.Token, AllowedOrigins: origins, WritePathConcurrencyLimit: s.Config.WriteConcurrency, WritePathOverloadMode: "defer_async"}, s, s, Uploads{s}, Deferred{s}, s.Config.BodyBytes, store.WithServiceCredential, func(ctx context.Context) error { return s.Check(ctx, false) })
	if err != nil {
		return err
	}
	var handler http.Handler = router
	if s.Config.Authority == "local-mysql-development" {
		handler, err = localdev.WithoutLogin(router, os.Getenv("LOCAL_MYSQL_NO_AUTH"), os.Getenv("APP_ORIGIN"), s.Config.Token)
		if err != nil {
			return err
		}
		logger.Warn("local MySQL development: HTTP authentication disabled; tenant scope retained")
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 30 * time.Second}
	if err = started(); err != nil {
		return err
	}
	finished := make(chan error, 1)
	go func() { finished <- server.Serve(listener) }()
	select {
	case err = <-finished:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), s.Config.Shutdown)
	defer cancel()
	err = server.Shutdown(shutdown)
	if err != nil {
		_ = server.Close()
	}
	serveErr := <-finished
	if !errors.Is(serveErr, http.ErrServerClosed) {
		return errors.Join(err, serveErr)
	}
	return err
}
func (s *Service) Worker(ctx context.Context, logger *slog.Logger, started func() error) error {
	queue, err := work.NewScoped(s.DB, s.Config.Database, "ingestion", 4096, s.Config.TenantID)
	if s.multiTenantLocal() {
		queue, err = work.New(s.DB, s.Config.Database, "ingestion", 4096)
	}
	if err != nil {
		return err
	}
	// Bind handlers before publishing readiness. Missing model, credentials or
	// queue configuration cannot leave a healthy no-op worker process.
	authorized := store.WithServiceCredential(ctx, s.Config.Token)
	if _, err = s.work(authorized, s.Config.TenantID, s.Config.ObjectType); err != nil {
		return err
	}
	if err = started(); err != nil {
		return err
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, name := range []string{s.Config.UploadQueue, s.Config.DeferredQueue} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			errs <- queue.Run(runCtx, work.RunnerConfig{Queue: name, Concurrency: s.Config.Concurrency, Lease: s.Config.Lease, Timeout: s.Config.Timeout, Poll: s.Config.Poll, RetryBase: time.Second, RetryMax: 10 * time.Second}, func(ctx context.Context, lease work.Lease) error {
				ctx = store.WithServiceCredential(ctx, s.Config.Token)
				tenant, object, err := s.resourceOwner(ctx, lease.ResourceID, lease.Kind)
				if err != nil {
					return err
				}
				if tenant != lease.TenantID {
					return work.ErrLease
				}
				w, err := s.work(ctx, tenant, object)
				if err != nil {
					return err
				}
				return w.Handle(ctx, lease)
			}, func(err error) { logger.Error("ingestion worker operation failed", "error", err) })
		}(name)
	}
	first := <-errs
	cancel()
	wg.Wait()
	second := <-errs
	if errors.Is(first, context.Canceled) {
		first = nil
	}
	if errors.Is(second, context.Canceled) {
		second = nil
	}
	return errors.Join(first, second)
}
