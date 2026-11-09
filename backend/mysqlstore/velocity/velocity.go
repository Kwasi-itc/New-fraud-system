// Package velocity implements ingestion-owned exact minute summaries. It never
// authorizes a tenant or resolves physical columns: the owning ingestion and
// decision adapters must hold their current publication/authority fences.
package velocity

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

const MaxGroupBytes = 256

type Definition struct {
	Tenant, Table, Version             uuid.UUID
	GroupField, TimeField, AmountField string
}

func Schema(database string) ([]string, error) {
	s, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	return []string{
		"CREATE TABLE " + s + ".ingestion_velocity_definitions(tenant_id BINARY(16) NOT NULL,table_id BINARY(16) NOT NULL,published_version BINARY(16) NOT NULL,group_field VARCHAR(128) NOT NULL,time_field VARCHAR(128) NOT NULL,amount_field VARCHAR(128) NOT NULL,PRIMARY KEY(tenant_id,table_id),UNIQUE KEY velocity_version(tenant_id,table_id,published_version)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin",
		"CREATE TABLE " + s + ".ingestion_velocity_buckets(tenant_id BINARY(16) NOT NULL,table_id BINARY(16) NOT NULL,published_version BINARY(16) NOT NULL,group_value VARBINARY(256) NOT NULL,bucket_at DATETIME(6) NOT NULL,row_count BIGINT NOT NULL,amount_count BIGINT NOT NULL,amount_sum DECIMAL(65,0) NOT NULL,PRIMARY KEY(tenant_id,table_id,published_version,group_value,bucket_at),CONSTRAINT velocity_definition_fk FOREIGN KEY(tenant_id,table_id,published_version) REFERENCES " + s + ".ingestion_velocity_definitions(tenant_id,table_id,published_version),CONSTRAINT velocity_nonnegative CHECK(row_count>=amount_count AND amount_count>=0)) ENGINE=InnoDB",
	}, nil
}

// Load locks an existing definition until the caller commits. Configuration
// changes/backfills additionally take the owner's physical registry exclusively.
func Load(ctx context.Context, tx *sql.Tx, database string, tenant, table, version uuid.UUID) (*Definition, error) {
	s, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	var v mysqlstore.UUID
	d := &Definition{Tenant: tenant, Table: table}
	err = tx.QueryRowContext(ctx, "SELECT published_version,group_field,time_field,amount_field FROM "+s+".ingestion_velocity_definitions WHERE tenant_id=? AND table_id=? FOR SHARE", mysqlstore.UUID(tenant), mysqlstore.UUID(table)).Scan(&v, &d.GroupField, &d.TimeField, &d.AmountField)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d.Version = uuid.UUID(v)
	if d.Version != version {
		return nil, nil
	} // Publication changes always fall back.
	if d.GroupField == "" || d.TimeField == "" || d.AmountField == "" || d.GroupField == d.TimeField || d.GroupField == d.AmountField || d.TimeField == d.AmountField {
		return nil, errors.New("invalid velocity definition")
	}
	return d, nil
}

type Contribution struct {
	Group   []byte
	At      time.Time
	Amount  *int32
	Present bool
}
type bucketKey struct {
	group string
	at    time.Time
}
type delta struct{ rows, amounts, sum int64 }
type Changes struct{ values map[bucketKey]delta }

func NewChanges() *Changes { return &Changes{values: make(map[bucketKey]delta)} }
func (c *Changes) Add(v Contribution, sign int64) error {
	if sign != 1 && sign != -1 {
		return errors.New("signed contribution required")
	}
	// Long or NULL groups/times are deliberately outside this projection.
	// Readers use it only for exact groups within the same byte bound.
	if !v.Present || len(v.Group) > MaxGroupBytes {
		return nil
	}
	if _, err := (mysqlstore.DateTime{Time: v.At}).Value(); err != nil {
		return err
	}
	k := bucketKey{string(v.Group), v.At.UTC().Truncate(time.Minute)}
	d := c.values[k]
	d.rows += sign
	if v.Amount != nil {
		d.amounts += sign
		d.sum += sign * int64(*v.Amount)
	}
	c.values[k] = d
	if len(c.values) > 1000 {
		return errors.New("velocity mutation bound exceeded")
	}
	return nil
}

// Apply is part of the SAME transaction as record writes and replay receipts.
// Sorting all affected groups/buckets gives corrections a consistent lock order.
func (c *Changes) Apply(ctx context.Context, tx *sql.Tx, database string, definition Definition) error {
	s, err := mysqlstore.Identifier(database)
	if err != nil {
		return err
	}
	keys := make([]bucketKey, 0, len(c.values))
	for k := range c.values {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].group != keys[j].group {
			return keys[i].group < keys[j].group
		}
		return keys[i].at.Before(keys[j].at)
	})
	for _, k := range keys {
		d := c.values[k]
		if d.rows == 0 && d.amounts == 0 && d.sum == 0 {
			continue
		}
		key := []any{mysqlstore.UUID(definition.Tenant), mysqlstore.UUID(definition.Table), mysqlstore.UUID(definition.Version), []byte(k.group), mysqlstore.DateTime{Time: k.at}}
		if d.rows > 0 && d.amounts >= 0 && d.rows >= d.amounts {
			args := append(append([]any{}, key...), d.rows, d.amounts, d.sum)
			_, err = tx.ExecContext(ctx, "INSERT INTO "+s+".ingestion_velocity_buckets(tenant_id,table_id,published_version,group_value,bucket_at,row_count,amount_count,amount_sum) VALUES(?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE row_count=row_count+?,amount_count=amount_count+?,amount_sum=amount_sum+?", append(args, d.rows, d.amounts, d.sum)...)
		} else {
			args := append([]any{d.rows, d.amounts, d.sum}, key...)
			args = append(args, d.rows, d.amounts)
			var result sql.Result
			result, err = tx.ExecContext(ctx, "UPDATE "+s+".ingestion_velocity_buckets SET row_count=row_count+?,amount_count=amount_count+?,amount_sum=amount_sum+? WHERE tenant_id=? AND table_id=? AND published_version=? AND group_value=? AND bucket_at=? AND row_count+?>=0 AND amount_count+?>=0", args...)
			if err == nil {
				n, e := result.RowsAffected()
				err = e
				if err == nil && n != 1 {
					err = errors.New("velocity summary missing or inconsistent")
				}
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// WholeMinutes returns an exact aligned interior, bounded to 24 hours. Two raw
// boundary fragments supply the remainder; an empty interior uses ordinary SQL.
func WholeMinutes(start, end time.Time) (time.Time, time.Time, bool) {
	start, end = start.UTC(), end.UTC()
	first := start.Truncate(time.Minute)
	if first.Before(start) {
		first = first.Add(time.Minute)
	}
	last := end.Truncate(time.Minute)
	return first, last, first.Before(last) && end.After(start) && end.Sub(start) <= 24*time.Hour
}

// AggregateExpression combines summary interior and raw edges in ONE SQL statement,
// giving READ COMMITTED callers one coherent data snapshot of atomic writes.
func AggregateExpression(database, qualified, expression, where, timeColumn string, d Definition, group []byte, start, end time.Time, predicateArgs []any, aggregate, field string) (string, []any, bool, error) {
	first, last, ok := WholeMinutes(start, end)
	if !ok || len(group) > MaxGroupBytes {
		return "", nil, false, nil
	}
	column := ""
	switch strings.ToLower(aggregate) {
	case "count":
		if field == "object_id" {
			column = "row_count"
		} else if field == d.AmountField {
			column = "amount_count"
		}
	case "sum":
		if field == d.AmountField {
			column = "amount_sum"
		}
	}
	if column == "" {
		return "", nil, false, nil
	}
	s, err := mysqlstore.Identifier(database)
	if err != nil {
		return "", nil, false, err
	}
	args := []any{mysqlstore.UUID(d.Tenant), mysqlstore.UUID(d.Table), mysqlstore.UUID(d.Version), group, mysqlstore.DateTime{Time: first}, mysqlstore.DateTime{Time: last}}
	args = append(args, predicateArgs...)
	args = append(args, mysqlstore.DateTime{Time: first}, mysqlstore.DateTime{Time: last})
	return "COALESCE((SELECT SUM(" + column + ") FROM " + s + ".ingestion_velocity_buckets WHERE tenant_id=? AND table_id=? AND published_version=? AND group_value=? AND bucket_at>=? AND bucket_at<?),0)+(SELECT " + expression + " FROM " + qualified + where + " AND (" + timeColumn + "<? OR " + timeColumn + ">=?))", args, true, nil
}
