package mysql

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/platform"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type RecordTagRepository struct{ platformRepository }
type RiskSnapshotRepository struct{ platformRepository }
type IPFlagRepository struct{ platformRepository }

var (
	_ ports.RecordTagRepository    = RecordTagRepository{}
	_ ports.RiskSnapshotRepository = RiskSnapshotRepository{}
	_ ports.IPFlagRepository       = IPFlagRepository{}
)

type platformRow struct {
	id, tenant string
	at         mysqlstore.DateTime
	text       []string
}

func (r platformRepository) scan(row rowScanner, fields int) (platformRow, int64, error) {
	var id, tenant mysqlstore.UUID
	var at mysqlstore.DateTime
	var size int64
	text := make([]mysqlstore.Text, fields)
	args := []any{&id, &tenant, &at, &size}
	for i := range text {
		args = append(args, &text[i])
	}
	if err := row.Scan(args...); err != nil {
		return platformRow{}, 0, err
	}
	values := make([]string, fields)
	for i := range text {
		values[i] = string(text[i])
	}
	if uuid.UUID(id) == uuid.Nil {
		return platformRow{}, 0, ErrTenantScope
	}
	if err := r.scope(uuid.UUID(tenant).String(), values...); err != nil {
		return platformRow{}, 0, err
	}
	return platformRow{uuid.UUID(id).String(), uuid.UUID(tenant).String(), at, values}, size, nil
}

func (r RecordTagRepository) scanTag(row rowScanner) (platform.RecordTag, int64, error) {
	v, size, err := r.scan(row, 3)
	if err != nil {
		return platform.RecordTag{}, 0, err
	}
	return platform.RecordTag{ID: v.id, TenantID: v.tenant, CreatedAt: v.at.Time, ObjectType: v.text[0], ObjectID: v.text[1], Tag: v.text[2]}, size, nil
}

func (r RecordTagRepository) selectTag() string {
	return "SELECT id,tenant_id,created_at," + r.projection("object_type", "object_id", "tag") + " FROM " + r.schema + ".decision_engine_record_tags"
}

func (r RecordTagRepository) Create(ctx context.Context, v platform.RecordTag) (platform.RecordTag, error) {
	id, err := r.creation(v.TenantID, v.ID, v.CreatedAt, v.ObjectType, v.ObjectID, v.Tag)
	if err != nil {
		return platform.RecordTag{}, err
	}
	_, err = r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".decision_engine_record_tags(id,tenant_id,object_type,object_id,tag,created_at) VALUES (?,?,?,?,?,?)", id, mysqlstore.UUID(r.tenant), mysqlstore.Text(v.ObjectType), mysqlstore.Text(v.ObjectID), mysqlstore.Text(v.Tag), mysqlstore.DateTime{Time: v.CreatedAt})
	if err != nil {
		return platform.RecordTag{}, err
	}
	// No RETURNING emulation outside the caller's transaction.
	return platformOne(r.platformRepository, r.tx.QueryRowContext(ctx, r.selectTag()+" WHERE tenant_id=? AND id=?", mysqlstore.UUID(r.tenant), id), r.scanTag)
}

func (r RecordTagRepository) ListByObject(ctx context.Context, tenant, objectType, objectID string) ([]platform.RecordTag, error) {
	if err := r.scope(tenant, objectType, objectID); err != nil {
		return nil, err
	}
	return platformList(ctx, r.platformRepository, r.selectTag()+" WHERE "+objectWhere+" ORDER BY created_at DESC,id DESC", r.objectArgs(objectType, objectID), r.scanTag)
}

func (r RecordTagRepository) HasTag(ctx context.Context, tenant, objectType, objectID, tag string) (bool, error) {
	if err := r.scope(tenant, objectType, objectID, tag); err != nil {
		return false, err
	}
	var found bool
	err := r.tx.QueryRowContext(ctx, r.hasTagQuery(), append(r.objectArgs(objectType, objectID), mysqlstore.Text(tag), mysqlstore.Text(tag))...).Scan(&found)
	return found, err
}

func (r RecordTagRepository) hasTagQuery() string {
	return "SELECT EXISTS(SELECT 1 FROM " + r.schema + ".decision_engine_record_tags WHERE " + objectWhere + " AND tag_sha=UNHEX(SHA2(CAST(? AS BINARY),256)) AND CAST(tag AS BINARY)=CAST(? AS BINARY))"
}

func (r RiskSnapshotRepository) scanRisk(row rowScanner) (platform.RiskSnapshot, int64, error) {
	v, size, err := r.scan(row, 3)
	if err != nil {
		return platform.RiskSnapshot{}, 0, err
	}
	return platform.RiskSnapshot{ID: v.id, TenantID: v.tenant, CreatedAt: v.at.Time, ObjectType: v.text[0], ObjectID: v.text[1], RiskLevel: v.text[2]}, size, nil
}

func (r RiskSnapshotRepository) selectRisk() string {
	return "SELECT id,tenant_id,created_at," + r.projection("object_type", "object_id", "risk_level") + " FROM " + r.schema + ".decision_engine_risk_snapshots"
}

func (r RiskSnapshotRepository) Create(ctx context.Context, v platform.RiskSnapshot) (platform.RiskSnapshot, error) {
	id, err := r.creation(v.TenantID, v.ID, v.CreatedAt, v.ObjectType, v.ObjectID, v.RiskLevel)
	if err != nil {
		return platform.RiskSnapshot{}, err
	}
	_, err = r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".decision_engine_risk_snapshots(id,tenant_id,object_type,object_id,risk_level,created_at) VALUES (?,?,?,?,?,?)", id, mysqlstore.UUID(r.tenant), mysqlstore.Text(v.ObjectType), mysqlstore.Text(v.ObjectID), mysqlstore.Text(v.RiskLevel), mysqlstore.DateTime{Time: v.CreatedAt})
	if err != nil {
		return platform.RiskSnapshot{}, err
	}
	return platformOne(r.platformRepository, r.tx.QueryRowContext(ctx, r.selectRisk()+" WHERE tenant_id=? AND id=?", mysqlstore.UUID(r.tenant), id), r.scanRisk)
}

func (r RiskSnapshotRepository) GetByObject(ctx context.Context, tenant, objectType, objectID string) (*platform.RiskSnapshot, error) {
	if err := r.scope(tenant, objectType, objectID); err != nil {
		return nil, err
	}
	v, err := platformOne(r.platformRepository, r.tx.QueryRowContext(ctx, r.selectRisk()+" WHERE "+objectWhere+" ORDER BY created_at DESC,id DESC LIMIT 1", r.objectArgs(objectType, objectID)...), r.scanRisk)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ports.ErrRiskSnapshotNotFound
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func (r IPFlagRepository) scanFlag(row rowScanner) (platform.IPFlag, int64, error) {
	v, size, err := r.scan(row, 2)
	if err != nil {
		return platform.IPFlag{}, 0, err
	}
	return platform.IPFlag{ID: v.id, TenantID: v.tenant, CreatedAt: v.at.Time, IPAddress: v.text[0], Flag: v.text[1]}, size, nil
}

func (r IPFlagRepository) selectFlag() string {
	return "SELECT id,tenant_id,created_at," + r.projection("ip_address", "flag") + " FROM " + r.schema + ".decision_engine_ip_flags"
}

func (r IPFlagRepository) Create(ctx context.Context, v platform.IPFlag) (platform.IPFlag, error) {
	id, err := r.creation(v.TenantID, v.ID, v.CreatedAt, v.IPAddress, v.Flag)
	if err != nil {
		return platform.IPFlag{}, err
	}
	_, err = r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".decision_engine_ip_flags(id,tenant_id,ip_address,flag,created_at) VALUES (?,?,?,?,?)", id, mysqlstore.UUID(r.tenant), mysqlstore.Text(v.IPAddress), mysqlstore.Text(v.Flag), mysqlstore.DateTime{Time: v.CreatedAt})
	if err != nil {
		return platform.IPFlag{}, err
	}
	return platformOne(r.platformRepository, r.tx.QueryRowContext(ctx, r.selectFlag()+" WHERE tenant_id=? AND id=?", mysqlstore.UUID(r.tenant), id), r.scanFlag)
}

const ipWhere = "tenant_id=? AND ip_address_sha=UNHEX(SHA2(CAST(? AS BINARY),256)) AND CAST(ip_address AS BINARY)=CAST(? AS BINARY)"

func (r IPFlagRepository) ListByIP(ctx context.Context, tenant, ip string) ([]platform.IPFlag, error) {
	if err := r.scope(tenant, ip); err != nil {
		return nil, err
	}
	return platformList(ctx, r.platformRepository, r.selectFlag()+" WHERE "+ipWhere+" ORDER BY created_at DESC,id DESC", []any{mysqlstore.UUID(r.tenant), mysqlstore.Text(ip), mysqlstore.Text(ip)}, r.scanFlag)
}

func (r IPFlagRepository) HasFlag(ctx context.Context, tenant, ip, flag string) (bool, error) {
	if err := r.scope(tenant, ip, flag); err != nil {
		return false, err
	}
	var found bool
	err := r.tx.QueryRowContext(ctx, r.hasFlagQuery(), mysqlstore.UUID(r.tenant), mysqlstore.Text(ip), mysqlstore.Text(ip), mysqlstore.Text(flag), mysqlstore.Text(flag)).Scan(&found)
	return found, err
}

func (r IPFlagRepository) hasFlagQuery() string {
	return "SELECT EXISTS(SELECT 1 FROM " + r.schema + ".decision_engine_ip_flags WHERE " + ipWhere + " AND flag_sha=UNHEX(SHA2(CAST(? AS BINARY),256)) AND CAST(flag AS BINARY)=CAST(? AS BINARY))"
}
