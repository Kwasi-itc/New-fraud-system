package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceingest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type crashBlockingReader struct{ marker string }

func (r crashBlockingReader) Read([]byte) (int, error) {
	if e := os.WriteFile(r.marker, []byte("blocked-after-prefix"), 0600); e != nil {
		return 0, e
	}
	select {}
}

// A separate process is killed while its bounded import is still reading. The
// ordinary test invocation never enters this test-only helper.
func TestSourceStageCrashHelper(t *testing.T) {
	dsn := os.Getenv("SCREENING_CRASH_HELPER_DSN")
	if dsn == "" {
		t.Skip("child process helper only")
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil || !strings.HasPrefix(cfg.ConnConfig.Database, "screening_delivery_test_") {
		t.Fatal("requires owned disposable database")
	}
	db, e := pgxpool.NewWithConfig(context.Background(), cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	records := []map[string]any{}
	for i := 0; i < 200; i++ {
		records = append(records, map[string]any{"id": fmt.Sprint(i), "name": "Fictional interrupted import", "type": "Person", "source_reference": "crash fixture"})
	}
	raw, _ := json.Marshal(struct {
		Version int    `json:"version"`
		Source  string `json:"source"`
		Records any    `json:"records"`
	}{1, "controlled-fixture", records})
	prefix := append(raw[:len(raw)-2], ',')
	_, e = (sourceingest.Store{DB: db}).Stage(context.Background(), sourceingest.Key{Owner: uuid.MustParse(os.Getenv("SCREENING_CRASH_HELPER_OWNER")), Dataset: "private-controlled"}, io.MultiReader(bytes.NewReader(prefix), crashBlockingReader{os.Getenv("SCREENING_CRASH_HELPER_MARKER")}))
	t.Fatal("parent should kill interrupted reader", e)
}
func TestSourceWorkerRecoversActualKilledImportProcess(t *testing.T) {
	binary := os.Getenv("SOURCE_WORKER_INTEGRATION_BINARY")
	if binary == "" {
		t.Skip("fresh source-worker binary required")
	}
	f := newInternalFixture(t)
	ctx := context.Background()
	marker := filepath.Join(t.TempDir(), "child-ready")
	cfg := f.db.Config().ConnConfig
	dbURL := url.URL{Scheme: "postgres", Host: net.JoinHostPort(cfg.Host, strconv.Itoa(int(cfg.Port))), Path: "/" + cfg.Database, User: url.UserPassword(cfg.User, cfg.Password)}
	dbURL.RawQuery = "sslmode=disable"
	child := exec.Command(os.Args[0], "-test.run=^TestSourceStageCrashHelper$")
	child.Env = append(os.Environ(), "SCREENING_CRASH_HELPER_DSN="+dbURL.String(), "SCREENING_CRASH_HELPER_OWNER="+f.a.Tenant().String(), "SCREENING_CRASH_HELPER_MARKER="+marker)
	if e := child.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { _ = child.Process.Kill() }()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, e := os.Stat(marker); e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child did not reach interrupted staging")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if e := child.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	_ = child.Wait()
	var staged int
	if e := f.db.QueryRow(ctx, `SELECT count(*) FROM screening.source_versions WHERE status='staging'`).Scan(&staged); e != nil || staged != 1 {
		t.Fatal("no durable abandoned stage", staged, e)
	}
	if _, e := f.db.Exec(ctx, `UPDATE screening.source_versions SET lease_until=now()-interval '1 second' WHERE status='staging'`); e != nil {
		t.Fatal(e)
	}
	// The separately deployed collector owns global sources only. It must leave
	// this tenant-private crash to its authorized private/operator recovery path.
	command := exec.Command(binary, "-mode", "once")
	command.Env = append(os.Environ(), "SOURCE_DATABASE_URL="+dbURL.String(), "SOURCE_DATABASE_URL_FILE=")
	if output, e := command.CombinedOutput(); e != nil {
		t.Fatalf("source worker restart: %s: %v", output, e)
	}
	if e := f.db.QueryRow(ctx, `SELECT count(*) FROM screening.source_versions WHERE status='staging'`).Scan(&staged); e != nil || staged != 1 {
		t.Fatal("global worker crossed private boundary", staged, e)
	}
	if n, e := (sourceingest.Store{DB: f.db}).RecoverInterrupted(ctx, 10); e != nil || n != 1 {
		t.Fatal("explicit owner recovery", n, e)
	}
	var active uuid.UUID
	if e := f.db.QueryRow(ctx, `SELECT active_generation FROM screening.source_datasets WHERE owner_scope=$1 AND id=$2`, f.private.Owner, f.private.Dataset).Scan(&active); e != nil || active != f.version {
		t.Fatal("crash changed active publication", active, e)
	}
}
