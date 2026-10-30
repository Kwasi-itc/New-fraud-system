// import-mysql-receipts processes trusted offline evidence only. It is not an API
// and never generates missing historical evidence or changes physical records.
package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	store "github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type checkpoint struct {
	Snapshot, InputSHA256, Target string
	Line                          int
}
type report struct {
	Read, WouldImport, AlreadyImported, Imported, Conflicts, Rejected int
	LastCommittedLine                                                 int
	HasMore                                                           bool
}

func saveCheckpoint(path string, value checkpoint) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".receipt-checkpoint-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if _, err = file.Write(append(data, '\n')); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
func run(ctx context.Context, args []string, out io.Writer) (result error) {
	flags := flag.NewFlagSet("import-mysql-receipts", flag.ContinueOnError)
	flags.SetOutput(out)
	mode := flags.String("mode", "dry-run", "dry-run or import")
	input := flags.String("input", "", "trusted JSONL export path")
	snapshot := flags.String("snapshot", "", "reviewed source snapshot identifier")
	database := flags.String("database", "", "shared target database")
	tenantText := flags.String("tenant", "", "authorized tenant UUID")
	tableText := flags.String("table", "", "stable target table UUID")
	object := flags.String("object-type", "", "published table name")
	statePath := flags.String("checkpoint", "", "durable local checkpoint path; required for import")
	fixture := flags.Bool("isolated-fixture", false, "permit only generated Compose fixture database")
	commandBytes := flags.Int("command-bytes", 1<<20, "approved target canonical/evidence byte bound")
	resultBytes := flags.Int("result-bytes", 1<<20, "approved target receipt response byte bound")
	keyBytes := flags.Int("key-bytes", 1024, "approved target public receipt key byte bound")
	collisionRows := flags.Int("collision-rows", 16, "approved target receipt bucket row bound")
	maxRecords := flags.Int("max-records", 1000, "bounded records per invocation, 1..10000")
	if err := flags.Parse(args); err != nil {
		return err
	}
	tenant, e1 := uuid.Parse(*tenantText)
	table, e2 := uuid.Parse(*tableText)
	if flags.NArg() != 0 || e1 != nil || e2 != nil || tenant == uuid.Nil || table == uuid.Nil || *input == "" || *snapshot == "" || len(*snapshot) > 256 || *object == "" || len(*object) > 1024 || (*mode != "dry-run" && *mode != "import") || (*mode == "import" && *statePath == "") || *maxRecords < 1 || *maxRecords > 10000 {
		return errors.New("bounded input, snapshot, target scope, mode and checkpoint required")
	}
	if *commandBytes < 1 || *commandBytes > 16<<20 || *resultBytes < 1 || *resultBytes > 16<<20 || *keyBytes < 1 || *keyBytes > 1<<20 || *collisionRows < 1 || *collisionRows > 65535 {
		return errors.New("invalid target receipt limits")
	}
	schema, err := mysqlstore.Identifier(*database)
	if err != nil {
		return err
	}
	file, err := os.Open(*input)
	if err != nil {
		return err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return err
	}
	if !stat.Mode().IsRegular() || stat.Size() > 1<<30 {
		return errors.New("regular export at most 1 GiB required")
	}
	staged, err := os.CreateTemp("", "ingestion-receipts-*")
	if err != nil {
		return err
	}
	defer os.Remove(staged.Name())
	defer staged.Close()
	digest := sha256.New()
	n, err := io.Copy(io.MultiWriter(digest, staged), io.LimitReader(file, (1<<30)+1))
	if err != nil {
		return err
	}
	if n > 1<<30 {
		return errors.New("export grew beyond bound")
	}
	if _, err = staged.Seek(0, io.SeekStart); err != nil {
		return err
	}
	address := os.Getenv("INGESTION_IMPORT_MYSQL_ADDRESS")
	user := os.Getenv("INGESTION_IMPORT_MYSQL_USER")
	password := os.Getenv("INGESTION_IMPORT_MYSQL_PASSWORD")
	token := os.Getenv("INGESTION_IMPORT_OPERATOR_TOKEN")
	if user == "" || strings.EqualFold(user, "root") || password == "" || len(token) < 32 {
		return errors.New("dedicated operator credentials and token required")
	}
	cfg := mysqlstore.Config{Address: address, Database: *database, User: user, Password: password, MaxOpen: 2, MaxIdle: 1, ConnectTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, ConnMaxLifetime: time.Minute, ConnMaxIdleTime: time.Minute}
	if *fixture {
		if address != "mysql:3306" || !regexp.MustCompile(`^ingestion_full_test_[a-f0-9]{32}$`).MatchString(*database) {
			return errors.New("only generated isolated Compose fixture permitted")
		}
		cfg.AllowInsecureTransport = true
	} else {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		cfg.TLS = &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	}
	expected := checkpoint{Snapshot: *snapshot, InputSHA256: hex.EncodeToString(digest.Sum(nil)), Target: address + "/" + *database + "/" + tenant.String() + "/" + table.String() + "/" + *object + fmt.Sprintf("/limits:%d:%d:%d:%d", *commandBytes, *resultBytes, *keyBytes, *collisionRows)}
	position := expected
	if *mode == "import" {
		var data []byte
		savedFile, e := os.Open(*statePath)
		if e == nil {
			data, e = io.ReadAll(io.LimitReader(savedFile, 4097))
			e = errors.Join(e, savedFile.Close())
		}
		if e == nil {
			if len(data) > 4096 {
				return errors.New("oversized checkpoint")
			}
			if e = json.Unmarshal(data, &position); e != nil {
				return e
			}
			saved := position
			saved.Line = 0
			if saved != expected || position.Line < 0 {
				return errors.New("checkpoint does not match immutable source and target")
			}
		}
		if e != nil && !errors.Is(e, os.ErrNotExist) {
			return e
		}
	}
	db, err := mysqlstore.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	limits := store.RecordOwnerLimits{Receipt: store.ReceiptLimits{KeyBytes: *keyBytes, ResultBytes: *resultBytes, CollisionRows: *collisionRows}, CommandBytes: *commandBytes, WriteBytes: 1 << 20, EffectRowBytes: 1 << 20, EffectBatchBytes: 4 << 20}
	owner, err := store.NewAuthorizedRecordOwner(db, *database, tenant, table, *object, token, limits, 16<<20)
	if err != nil {
		return err
	}
	importer, err := store.NewLegacyImporter(owner, func(ctx context.Context, tx *sql.Tx, r store.ReceiptRequest) error {
		if r.TenantID != tenant || r.TableID != table {
			return store.ErrOwnerAuthorization
		}
		// The dedicated SQL principal is the mutation authority. Check the retained
		// provenance capability even for a first import; no caller supplies an actor.
		rows, e := tx.QueryContext(ctx, "SELECT source_evidence_sha256 FROM "+schema+".ingestion_legacy_imports WHERE tenant_id=? LIMIT 0", mysqlstore.UUID(tenant))
		if e != nil {
			return e
		}
		return rows.Close()
	})
	if err != nil {
		return err
	}
	ctx = store.WithServiceCredential(ctx, token)
	summary := report{LastCommittedLine: position.Line}
	defer func() { result = errors.Join(result, json.NewEncoder(out).Encode(summary)) }()
	scanner := bufio.NewScanner(staged)
	scanner.Buffer(make([]byte, 64<<10), 16<<20)
	line := 0
	for scanner.Scan() {
		line++
		if *mode == "import" && line <= position.Line {
			continue
		}
		if summary.Read == *maxRecords {
			summary.HasMore = true
			break
		}
		summary.Read++
		var in store.LegacyImport
		if e := json.Unmarshal(scanner.Bytes(), &in); e != nil || !utf8.Valid(scanner.Bytes()) {
			summary.Rejected++
			if *mode == "import" {
				return fmt.Errorf("rejected evidence at line %d", line)
			}
			continue
		}
		status, e := importer.Preview(ctx, in)
		if e != nil {
			switch {
			case errors.Is(e, store.ErrReceiptConflict), errors.Is(e, store.ErrReceiptLimit):
				summary.Conflicts++
			case errors.Is(e, store.ErrLegacyRejected):
				summary.Rejected++
			default:
				return fmt.Errorf("preview line %d: %w", line, e)
			}
			if *mode == "import" {
				return fmt.Errorf("import stopped at rejected/conflicting line %d", line)
			}
			continue
		}
		if status == "already_imported" {
			summary.AlreadyImported++
		} else if *mode == "dry-run" {
			summary.WouldImport++
		}
		if *mode == "dry-run" {
			continue
		}
		result, e := importer.Import(ctx, in)
		if e != nil {
			return fmt.Errorf("import line %d: %w", line, e)
		}
		if !result.Replayed {
			summary.Imported++
		} else if status != "already_imported" {
			summary.AlreadyImported++
		}
		position.Line = line
		if e := saveCheckpoint(*statePath, position); e != nil {
			return fmt.Errorf("commit succeeded; replay required before checkpoint: %w", e)
		}
		summary.LastCommittedLine = line
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if line < position.Line {
		return errors.New("checkpoint beyond export length")
	}
	return nil
}
func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
