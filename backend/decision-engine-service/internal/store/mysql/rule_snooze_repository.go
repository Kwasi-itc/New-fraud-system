package mysql

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/snooze"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
	"time"
)

type RuleSnoozeRepository struct{ authoringRepository }

var _ ports.RuleSnoozeRepository = RuleSnoozeRepository{}

func (r RuleSnoozeRepository) selectSQL() string {
	return "SELECT id,tenant_id,scenario_id,snooze_group_id,created_at,expires_at," + r.projection("object_type", "object_id") + " FROM " + r.schema + ".decision_engine_rule_snoozes"
}
func (r RuleSnoozeRepository) scan(row rowScanner) (snooze.RuleSnooze, int64, error) {
	var id, tenant, parent, group mysqlstore.UUID
	var created, expires mysqlstore.DateTime
	var size int64
	var kind, object mysqlstore.Text
	if err := row.Scan(&id, &tenant, &parent, &group, &created, &expires, &size, &kind, &object); err != nil {
		return snooze.RuleSnooze{}, 0, err
	}
	return snooze.RuleSnooze{ID: uuid.UUID(id).String(), TenantID: uuid.UUID(tenant).String(), ScenarioID: uuid.UUID(parent).String(), SnoozeGroupID: uuid.UUID(group).String(), CreatedAt: created.Time, ExpiresAt: expires.Time, ObjectType: string(kind), ObjectID: string(object)}, size, nil
}
func (r RuleSnoozeRepository) Create(ctx context.Context, v snooze.RuleSnooze) (snooze.RuleSnooze, error) {
	if err := v.Validate(); err != nil {
		return snooze.RuleSnooze{}, err
	}
	id, err := r.creation(v.TenantID, v.ID, v.CreatedAt, v.ObjectType, v.ObjectID)
	if err != nil {
		return snooze.RuleSnooze{}, err
	}
	args, err := r.ids(v.TenantID, v.ScenarioID, v.SnoozeGroupID)
	if err != nil {
		return snooze.RuleSnooze{}, err
	}
	if _, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".decision_engine_rule_snoozes(id,tenant_id,scenario_id,object_type,object_id,snooze_group_id,created_at,expires_at) VALUES(?,?,?,?,?,?,?,?)", id, args[0], args[1], mysqlstore.Text(v.ObjectType), mysqlstore.Text(v.ObjectID), args[2], mysqlstore.DateTime{Time: v.CreatedAt}, mysqlstore.DateTime{Time: v.ExpiresAt}); err != nil {
		return snooze.RuleSnooze{}, err
	}
	return platformOne(r.platformRepository, r.tx.QueryRowContext(ctx, r.selectSQL()+" WHERE tenant_id=? AND id=?", args[0], id), r.scan)
}
func (r RuleSnoozeRepository) ListActive(ctx context.Context, tenant, parent, kind, object string, now time.Time) ([]snooze.RuleSnooze, error) {
	ids, err := r.ids(tenant, parent)
	if err != nil {
		return nil, err
	}
	if err := r.scope(tenant, kind, object); err != nil {
		return nil, err
	}
	args := []any{ids[0], ids[1], mysqlstore.Text(kind), mysqlstore.Text(object), mysqlstore.Text(kind), mysqlstore.Text(object), mysqlstore.DateTime{Time: now}}
	return platformList(ctx, r.platformRepository, r.selectSQL()+" WHERE tenant_id=? AND scenario_id=? AND object_type_sha=UNHEX(SHA2(CAST(? AS BINARY),256)) AND object_id_sha=UNHEX(SHA2(CAST(? AS BINARY),256)) AND CAST(object_type AS BINARY)=CAST(? AS BINARY) AND CAST(object_id AS BINARY)=CAST(? AS BINARY) AND expires_at>? ORDER BY expires_at DESC,id DESC", args, r.scan)
}
