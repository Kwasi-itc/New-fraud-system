package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/velocity"
)

// WithVelocitySummaries opts this trusted owner into installed projection
// metadata. No definition means ordinary record writes. Published-version
// changes disable the old projection until its explicit operator rebuild.
func (o *KeyedRecordOwner) WithVelocitySummaries() *KeyedRecordOwner {
	copy := *o
	copy.velocity = true
	return &copy
}

func (o *KeyedRecordOwner) RebuildVelocity(ctx context.Context, definition velocity.Definition, maxRows int) error {
	if definition.Tenant != o.receipts.tenant || definition.Table != o.table {
		return ErrOwnerAuthorization
	}
	request := ReceiptRequest{TenantID: definition.Tenant, TableID: definition.Table}
	if err := o.receipts.authorize(ctx, request); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return mysqlstore.Mutation(ctx, o.receipts.db, func(tx *sql.Tx) error {
		if err := o.receipts.authorizeTx(ctx, tx, request); err != nil {
			return err
		}
		model, err := o.load(ctx, tx, request)
		if err != nil {
			return err
		}
		writer, err := NewRecordWriter(ctx, tx, o.database, definition.Tenant, model, o.objectType, definition.Version)
		if err != nil {
			return err
		}
		return writer.RebuildVelocity(ctx, definition, maxRows)
	})
}

func (w *RecordWriter) withVelocity(ctx context.Context) error {
	d, err := velocity.Load(ctx, w.tx, w.database, w.tenant, w.table, w.version)
	if err != nil {
		return err
	}
	if d == nil {
		return nil
	}
	for name, kind := range map[string]string{d.GroupField: "string", d.TimeField: "timestamp", d.AmountField: "int"} {
		field, ok := w.fields[name]
		if !ok || field.definition.Archived || field.definition.DataType != kind {
			return errors.New("velocity definition incompatible with publication")
		}
	}
	w.velocity = d
	return nil
}

func (w *RecordWriter) velocityContribution(ctx context.Context, id mysqlstore.UUID) (velocity.Contribution, error) {
	d := w.velocity
	if d == nil {
		return velocity.Contribution{}, nil
	}
	column := func(name string) string { return "`" + w.fields[name].physical.Components["value"] + "`" }
	var group []byte
	var at sql.NullTime
	var amount sql.NullInt64
	err := w.tx.QueryRowContext(ctx, "SELECT "+column(d.GroupField)+","+column(d.TimeField)+","+column(d.AmountField)+" FROM "+w.qualified+" WHERE id=?", id).Scan(&group, &at, &amount)
	if err != nil {
		return velocity.Contribution{}, err
	}
	value := velocity.Contribution{Group: group, At: at.Time, Present: group != nil && at.Valid}
	if amount.Valid {
		if amount.Int64 < -2147483648 || amount.Int64 > 2147483647 {
			return value, ErrWriteMapping
		}
		n := int32(amount.Int64)
		value.Amount = &n
	}
	return value, nil
}

// RebuildVelocity is an explicit trusted operator operation, not a request DTO.
// Its role needs definition/bucket management privileges. The physical registry
// is locked exclusively before backfill, so in-flight writers drain and new
// writers cannot miss definition activation. No partial projection is exposed.
// Call only inside an authorized owner transaction; normal runtime roles cannot
// change definitions or delete summaries.
func (w *RecordWriter) RebuildVelocity(ctx context.Context, d velocity.Definition, maxRows int) error {
	if d.Tenant != w.tenant || d.Table != w.table || d.Version != w.version || maxRows < 1 || maxRows > 100000 || d.GroupField == d.TimeField || d.GroupField == d.AmountField || d.TimeField == d.AmountField {
		return errors.New("bounded matching velocity scope required")
	}
	s, err := mysqlstore.Identifier(w.database)
	if err != nil {
		return err
	}
	var version mysqlstore.UUID
	var state string
	if err := w.tx.QueryRowContext(ctx, "SELECT published_version,write_state FROM "+s+".data_model_physical_tables WHERE tenant_id=? AND table_id=? FOR UPDATE", mysqlstore.UUID(w.tenant), mysqlstore.UUID(w.table)).Scan(&version, &state); err != nil {
		return err
	}
	if state != "open" || version != mysqlstore.UUID(w.version) {
		return ErrWriteUnavailable
	}
	for name, kind := range map[string]string{d.GroupField: "string", d.TimeField: "timestamp", d.AmountField: "int"} {
		f, ok := w.fields[name]
		if !ok || f.definition.Archived || f.definition.DataType != kind || len(name) > 128 {
			return ErrWriteMapping
		}
	}
	// Grouped backfill is complete or rejected. LIMIT bounds the eligibility
	// check, never the records used by an accepted summary.
	var count int
	if err := w.tx.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM(SELECT 1 FROM %s LIMIT %d) velocity_source", w.qualified, maxRows+1)).Scan(&count); err != nil {
		return err
	}
	if count > maxRows {
		return errors.New("velocity backfill exceeds maintenance bound")
	}
	if err := w.tx.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM(SELECT 1 FROM %s.ingestion_velocity_buckets WHERE tenant_id=? AND table_id=? LIMIT %d) old_velocity", s, maxRows+1), mysqlstore.UUID(w.tenant), mysqlstore.UUID(w.table)).Scan(&count); err != nil {
		return err
	}
	if count > maxRows {
		return errors.New("velocity replacement exceeds maintenance bound")
	}
	if _, err = w.tx.ExecContext(ctx, "DELETE FROM "+s+".ingestion_velocity_buckets WHERE tenant_id=? AND table_id=?", mysqlstore.UUID(w.tenant), mysqlstore.UUID(w.table)); err != nil {
		return err
	}
	if _, err = w.tx.ExecContext(ctx, "INSERT INTO "+s+".ingestion_velocity_definitions VALUES(?,?,?,?,?,?) ON DUPLICATE KEY UPDATE published_version=?,group_field=?,time_field=?,amount_field=?", mysqlstore.UUID(w.tenant), mysqlstore.UUID(w.table), mysqlstore.UUID(w.version), d.GroupField, d.TimeField, d.AmountField, mysqlstore.UUID(w.version), d.GroupField, d.TimeField, d.AmountField); err != nil {
		return err
	}
	column := func(name string) string { return "`" + w.fields[name].physical.Components["value"] + "`" }
	group, at, amount := column(d.GroupField), column(d.TimeField), column(d.AmountField)
	// String formatting is limited to checked owner physical identifiers; all
	// tenant/version values are parameters. Binary groups preserve complete bytes.
	bucket := "TIMESTAMP(DATE_FORMAT(" + at + ",'%Y-%m-%d %H:%i:00'))"
	statement := "INSERT INTO " + s + ".ingestion_velocity_buckets(tenant_id,table_id,published_version,group_value,bucket_at,row_count,amount_count,amount_sum) SELECT ?,?,?,CAST(" + group + " AS BINARY)," + bucket + ",COUNT(*),COUNT(" + amount + "),COALESCE(SUM(" + amount + "),0) FROM " + w.qualified + " WHERE " + group + " IS NOT NULL AND OCTET_LENGTH(" + group + ")<=256 AND " + at + " IS NOT NULL GROUP BY CAST(" + group + " AS BINARY)," + bucket
	_, err = w.tx.ExecContext(ctx, statement, mysqlstore.UUID(w.tenant), mysqlstore.UUID(w.table), mysqlstore.UUID(w.version))
	return err
}
