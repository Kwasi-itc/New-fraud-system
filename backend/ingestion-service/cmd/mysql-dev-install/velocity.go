package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"

	R "github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/mysqlruntime"
	store "github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/velocity"
	"github.com/google/uuid"
)

func configureVelocity(ctx context.Context, db *sql.DB, c R.Config, table, version uuid.UUID) error {
	if os.Getenv("LOCAL_INGESTION_CONFIGURE_VELOCITY") != "1" {
		return nil
	}
	file, err := os.Open("/runtime/velocity-definition.json")
	if err != nil {
		return err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return err
	}
	if len(raw) > 4096 {
		return errors.New("velocity definition exceeds bound")
	}
	var definition struct {
		GroupField  string `json:"group_field"`
		TimeField   string `json:"time_field"`
		AmountField string `json:"amount_field"`
		MaxRows     int    `json:"max_rows"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&definition); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("one velocity definition required")
	}
	owner, err := store.NewAuthorizedRecordOwner(db, c.Database, c.TenantID, table, c.ObjectType, c.Token, c.Limits, c.ContractBytes)
	if err != nil {
		return err
	}
	return owner.RebuildVelocity(store.WithServiceCredential(ctx, c.Token), velocity.Definition{Tenant: c.TenantID, Table: table, Version: version, GroupField: definition.GroupField, TimeField: definition.TimeField, AmountField: definition.AmountField}, definition.MaxRows)
}
