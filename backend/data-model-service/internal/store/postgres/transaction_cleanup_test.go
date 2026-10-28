package postgres

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/access"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/ports"
)

func TestTransactionManagerRequiresDependencies(t *testing.T) {
	called := false
	if err := NewTransactionManager(nil).Run(context.Background(), func(ports.MutationStore) error {
		called = true
		return nil
	}); err == nil || called {
		t.Fatal("missing database must fail before callback")
	}
	if err := NewTransactionManager(&pgxpool.Pool{}).Run(context.Background(), nil); err == nil {
		t.Fatal("missing callback accepted")
	}
}

func TestPostgresTransactionCleanup(t *testing.T) {
	rawURL := os.Getenv("DATA_MODEL_TX_TEST_DATABASE_URL")
	if rawURL == "" {
		if os.Getenv("DATA_MODEL_REQUIRE_TX_TEST") == "1" {
			t.Fatal("transaction test database configuration required")
		}
		t.Skip("run compose.migration-test.yaml for disposable transaction verification")
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1") || !strings.HasPrefix(u.Path, "/data_model_tx_test_") {
		t.Fatal("transaction fixture requires a local data_model_tx_test_* database")
	}
	ctx, stop := context.WithTimeout(context.Background(), 30*time.Second)
	defer stop()
	cfg, err := pgxpool.ParseConfig(rawURL)
	if err != nil {
		t.Fatal("invalid test connection configuration")
	}
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	name := pgx.Identifier{fmt.Sprintf("transaction_cleanup_%d", time.Now().UnixNano())}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE TABLE "+name+" (id TEXT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	manager := NewTransactionManager(pool)
	sentinel := errors.New("operation failed after write")
	for _, scenario := range []string{"commit", "error", "cancel_error", "cancel_success", "panic", "read_only_grant"} {
		t.Run(scenario, func(t *testing.T) {
			request, cancel := context.WithCancel(ctx)
			defer cancel()
			if scenario == "read_only_grant" {
				request = access.WithGrant(request, access.Grant{Write: false})
			}
			calls := 0
			var result error
			var recovered any
			func() {
				defer func() { recovered = recover() }()
				result = manager.Run(request, func(store ports.MutationStore) error {
					calls++
					if _, err := store.(mutationStore).rawTx.Exec(request, "INSERT INTO "+name+" VALUES ($1)", scenario); err != nil {
						return err
					}
					switch scenario {
					case "error":
						return sentinel
					case "cancel_error":
						cancel()
						return sentinel
					case "cancel_success":
						cancel()
					case "panic":
						panic(sentinel)
					}
					return nil
				})
			}()
			switch scenario {
			case "commit":
				if result != nil {
					t.Fatal(result)
				}
			case "error", "cancel_error":
				if !errors.Is(result, sentinel) {
					t.Fatalf("original callback error lost: %v", result)
				}
			case "cancel_success":
				if !errors.Is(result, context.Canceled) {
					t.Fatalf("canceled callback committed or lost cancellation: %v", result)
				}
			case "panic":
				if recovered != sentinel {
					t.Fatalf("panic changed: %v", recovered)
				}
			case "read_only_grant":
				if result == nil || calls != 0 {
					t.Fatal("read-only grant reached mutation callback")
				}
			}
			if scenario != "panic" && recovered != nil {
				t.Fatalf("unexpected panic: %v", recovered)
			}
			if scenario != "read_only_grant" && calls != 1 {
				t.Fatalf("unexpected callback count: %d", calls)
			}
			// A single-slot pool exposes leaked transactions as acquisition failure.
			verify, finish := context.WithTimeout(ctx, 2*time.Second)
			defer finish()
			var count int
			if err := pool.QueryRow(verify, "SELECT count(*) FROM "+name+" WHERE id=$1", scenario).Scan(&count); err != nil {
				t.Fatalf("pool unusable after transaction: %v", err)
			}
			want := 0
			if scenario == "commit" {
				want = 1
			}
			if count != want {
				t.Fatalf("persisted rows=%d, want %d", count, want)
			}
			if err := manager.Run(verify, func(store ports.MutationStore) error {
				_, err := store.(mutationStore).rawTx.Exec(verify, "SELECT 1")
				return err
			}); err != nil {
				t.Fatalf("subsequent transaction failed: %v", err)
			}
		})
	}
}
