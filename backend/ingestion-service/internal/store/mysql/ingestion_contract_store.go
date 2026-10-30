package mysql

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"regexp"
	"unicode/utf8"

	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

var ErrContractBounds = errors.New("published contract exceeds configured transfer bound")

// IngestionContractStore consumes the data-model v006 fixed-domain lookup.
// It provides metadata, not caller authorization. Cache hits still re-read status,
// archive, publication gate and stable identity on every request.
type IngestionContractStore struct {
	db       *sql.DB
	schema   string
	maxBytes int
}

var _ ports.IngestionContractStore = (*IngestionContractStore)(nil)

func NewIngestionContractStore(db *sql.DB, database string, maxBytes int) (*IngestionContractStore, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	if db == nil || maxBytes < 1 || maxBytes > 16<<20 {
		return nil, ErrContractBounds
	}
	return &IngestionContractStore{db, schema, maxBytes}, nil
}
func (s *IngestionContractStore) ReadContract(ctx context.Context, tenant uuid.UUID, name string, knownTable, knownVersion uuid.UUID) (ports.PublishedContract, error) {
	return s.read(ctx, s.db, tenant, name, knownTable, knownVersion)
}

type contractQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *IngestionContractStore) read(ctx context.Context, q contractQuerier, tenant uuid.UUID, name string, knownTable, knownVersion uuid.UUID) (ports.PublishedContract, error) {
	var zero ports.PublishedContract
	if tenant == uuid.Nil || len(name) == 0 || len(name) > 1<<20 || !utf8.ValidString(name) {
		return zero, ErrContractBounds
	}
	// Resolve the immutable key separately: a NOT DETERMINISTIC function in
	// the join predicate cannot provide indexed lookup on MySQL 8.4.8.
	var key mysqlstore.NullUUID
	if err := q.QueryRowContext(ctx, "SELECT "+s.schema+".data_model_find_ingestion_contract_key_v1(?,?)", mysqlstore.UUID(tenant), []byte(name)).Scan(&key); err != nil {
		return zero, err
	}
	state := ports.PublishedContract{TenantID: tenant}
	var table, version, physicalVersion mysqlstore.NullUUID
	var archived sql.NullBool
	var gate sql.NullString
	var payloadBytes sql.NullInt64
	var schemaBytes int64
	// The unique exact-key ID is found by a fixed-domain owner routine. The join
	// cannot scan a tenant's first N metadata entries or read the private dictionary.
	query := s.contractReadSQL()
	err := q.QueryRowContext(ctx, query, s.maxBytes, mysqlstore.UUID(knownTable), mysqlstore.UUID(knownVersion), s.maxBytes, key, mysqlstore.UUID(tenant)).Scan(&state.SchemaName, &schemaBytes, &state.TenantStatus, &table, &version, &archived, &gate, &physicalVersion, &payloadBytes, &state.Payload)
	if err != nil {
		return zero, err
	}
	if schemaBytes > int64(s.maxBytes) {
		return zero, ErrContractBounds
	}
	if !table.Valid {
		return state, nil
	}
	if !version.Valid || uuid.UUID(version.UUID) == uuid.Nil || uuid.UUID(table.UUID) == uuid.Nil || !archived.Valid || archived.Bool || !gate.Valid || gate.String != "open" || !physicalVersion.Valid || physicalVersion.UUID != version.UUID {
		return zero, ErrWriteUnavailable
	}
	if !payloadBytes.Valid || payloadBytes.Int64 > int64(s.maxBytes) {
		return zero, ErrContractBounds
	}
	state.TableID = uuid.UUID(table.UUID)
	state.Version = uuid.UUID(version.UUID)
	state.TableName = name
	return state, nil
}

// ResolveRecordOwner authenticates before metadata access and binds a route name
// to the current stable table. Each owner operation rechecks that binding inside
// its transaction, so a rename/recreation cannot redirect an existing receipt.
func ResolveRecordOwner(ctx context.Context, db *sql.DB, database string, tenant uuid.UUID, name, serviceToken string, limits RecordOwnerLimits, maxContractBytes int) (*KeyedRecordOwner, error) {
	token, _ := ctx.Value(credentialContextKey{}).(string)
	if serviceToken == "" || token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(serviceToken)) != 1 {
		return nil, ErrOwnerAuthorization
	}
	store, err := NewIngestionContractStore(db, database, maxContractBytes)
	if err != nil {
		return nil, err
	}
	state, err := store.ReadContract(ctx, tenant, name, uuid.Nil, uuid.Nil)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrOwnerAuthorization
	}
	if err != nil {
		return nil, err
	}
	if state.TenantStatus != "active" || state.TableID == uuid.Nil {
		return nil, ErrOwnerAuthorization
	}
	return NewAuthorizedRecordOwner(db, database, tenant, state.TableID, name, serviceToken, limits, maxContractBytes)
}

// ContractReadGrants grants only the documented metadata contract and fixed-domain
// routine. Physical record SELECT permissions are granted separately per table.
func ContractReadGrants(database, role string) ([]string, error) {
	s, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`).MatchString(role) || role == "root" {
		return nil, errors.New("dedicated contract reader role required")
	}
	r, err := mysqlstore.Identifier(role)
	if err != nil {
		return nil, err
	}
	var grants []string
	for _, name := range []string{"data_model_tenants", "data_model_ingestion_contracts", "data_model_tables", "data_model_physical_tables", "data_model_fields", "data_model_physical_fields"} {
		grants = append(grants, "GRANT SELECT ON "+s+"."+name+" TO "+r)
	}
	grants = append(grants, "GRANT EXECUTE ON FUNCTION "+s+".data_model_find_ingestion_contract_key_v1 TO "+r)
	return grants, nil
}

func (s *IngestionContractStore) contractReadSQL() string {
	return "SELECT CASE WHEN OCTET_LENGTH(t.schema_name)<=? THEN t.schema_name ELSE '' END,OCTET_LENGTH(t.schema_name),t.status,c.table_id,c.version,m.archived,p.write_state,p.published_version,OCTET_LENGTH(c.payload),CASE WHEN c.table_id=? AND c.version=? THEN NULL WHEN OCTET_LENGTH(c.payload)<=? THEN c.payload ELSE NULL END FROM " + s.schema + ".data_model_tenants t LEFT JOIN " + s.schema + ".data_model_ingestion_contracts c ON c.tenant_id=t.id AND c.table_name_key_id=? LEFT JOIN " + s.schema + ".data_model_tables m ON m.tenant_id=c.tenant_id AND m.id=c.table_id LEFT JOIN " + s.schema + ".data_model_physical_tables p ON p.tenant_id=c.tenant_id AND p.table_id=c.table_id WHERE t.id=?"
}
