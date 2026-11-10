package mysql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"strings"
)

var ErrLimit = errors.New("collection exceeds the supported limit; use a narrower scope")

type Query interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
type Store struct {
	q      Query
	db     *sql.DB
	schema string
}

func New(db *sql.DB, database string) (*Store, error) {
	s, e := mysqlstore.Identifier(database)
	if e != nil {
		return nil, e
	}
	if db == nil {
		return nil, errors.New("database required")
	}
	return &Store{q: db, db: db, schema: s}, nil
}
func (s *Store) InTransaction(tx *sql.Tx) *Store { return &Store{q: tx, db: s.db, schema: s.schema} }
func (s *Store) table(name string) string        { return s.schema + ".`screening_" + name + "`" }
func (s *Store) Run(ctx context.Context, fn func(ports.MutationStore) error) error {
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if e != nil {
		return normalize(e)
	}
	defer tx.Rollback()
	if e = s.InTransaction(tx).authority(ctx, true); e != nil {
		return e
	}
	if e = fn(s.InTransaction(tx)); e != nil {
		return normalize(e)
	}
	return normalize(tx.Commit())
}

// Native work is durable in pending domain rows. The MySQL worker polls those
// rows; it never passes a SQL transaction to PostgreSQL's River client.
func (s *Store) RawTx() pgx.Tx                                       { return nil }
func (s *Store) Screenings() ports.ScreeningRepository               { return screenings{s} }
func (s *Store) ScreeningMatches() ports.ScreeningMatchRepository    { return matches{s} }
func (s *Store) ScreeningComments() ports.ScreeningCommentRepository { return comments{s} }
func (s *Store) Whitelist() ports.ScreeningWhitelistRepository       { return whitelist{s} }
func (s *Store) ScreeningFiles() ports.ScreeningFileRepository       { return files{s} }
func (s *Store) ContinuousConfigs() ports.ContinuousConfigRepository { return configs{s} }
func (s *Store) MonitoredObjects() ports.MonitoredObjectRepository   { return monitored{s} }
func (s *Store) DatasetUpdateJobs() ports.DatasetUpdateJobRepository { return jobs{s} }
func (s *Store) CaseEvents() ports.CaseEventRepository               { return outbox{s} }
func (s *Store) ObjectExclusions() ports.ObjectExclusionRepository   { return exclusions{s} }

type screenings struct{ *Store }
type matches struct{ *Store }
type comments struct{ *Store }
type whitelist struct{ *Store }
type files struct{ *Store }
type configs struct{ *Store }
type monitored struct{ *Store }
type jobs struct{ *Store }
type outbox struct{ *Store }
type exclusions struct{ *Store }
type identifier string

func (v identifier) Value() (driver.Value, error) {
	u, e := uuid.Parse(string(v))
	if e != nil || u == uuid.Nil {
		return nil, errors.New("valid UUID required")
	}
	return mysqlstore.UUID(u).Value()
}
func id(v string) identifier { return identifier(v) }
func nullableID(v string) any {
	if v == "" {
		return nil
	}
	return id(v)
}

type uuidString struct{ p *string }

func (v uuidString) Scan(src any) error {
	if src == nil {
		*v.p = ""
		return nil
	}
	var u mysqlstore.UUID
	if e := u.Scan(src); e != nil {
		return e
	}
	*v.p = uuid.UUID(u).String()
	return nil
}
func uid(p *string) uuidString { return uuidString{p} }
func matchKey(v string) string {
	if u, e := uuid.Parse(v); e == nil {
		return u.String()
	}
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("screening-match/v1/"+v)).String()
}
func jsonValue(v []byte) mysqlstore.JSON {
	if len(v) == 0 {
		return mysqlstore.JSON(`{}`)
	}
	return mysqlstore.JSON(v)
}
func normalize(e error) error {
	if errors.Is(e, sql.ErrNoRows) {
		return pgx.ErrNoRows
	}
	var m *mysqlDriver.MySQLError
	if errors.As(e, &m) {
		switch m.Number {
		case 1062:
			return errors.New("resource identity already exists")
		case 1205, 1213:
			return ports.ErrScreeningBusy
		case 1048, 1406, 1451, 1452, 3819, 1644:
			return errors.New("screening validation or relationship conflict")
		default:
			return errors.New("screening storage unavailable")
		}
	}
	return e
}

type scanner interface{ Scan(...any) error }

func collect[T any](rows *sql.Rows, e error, max int, scan func(scanner) (T, error)) ([]T, error) {
	if e != nil {
		return nil, normalize(e)
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		if len(out) >= max {
			return nil, ErrLimit
		}
		v, e := scan(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, normalize(rows.Err())
}
func list[T any](ctx context.Context, s *Store, q string, args []any, max int, scan func(scanner) (T, error)) ([]T, error) {
	rows, e := s.q.QueryContext(ctx, q, args...)
	return collect(rows, e, max, scan)
}
func batch(n int) int {
	if n < 1 {
		return 50
	}
	if n > 500 {
		return 500
	}
	return n
}
func placeholders(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }
func encoded(v any) (mysqlstore.JSON, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return nil, e
	}
	if len(b) > 1<<20 {
		return nil, fmt.Errorf("payload exceeds 1 MiB")
	}
	return mysqlstore.JSON(b), nil
}

var _ ports.MutationStore = (*Store)(nil)
