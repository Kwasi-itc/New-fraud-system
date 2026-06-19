package postgres

import (
	"context"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/platform"
)

type CustomListRepository struct{ q queryable }
type RecordTagRepository struct{ q queryable }
type RiskSnapshotRepository struct{ q queryable }
type IPFlagRepository struct{ q queryable }

func NewCustomListRepository(q queryable) CustomListRepository { return CustomListRepository{q: q} }
func NewRecordTagRepository(q queryable) RecordTagRepository   { return RecordTagRepository{q: q} }
func NewRiskSnapshotRepository(q queryable) RiskSnapshotRepository {
	return RiskSnapshotRepository{q: q}
}
func NewIPFlagRepository(q queryable) IPFlagRepository { return IPFlagRepository{q: q} }

func (r CustomListRepository) CreateList(ctx context.Context, item platform.CustomList) (platform.CustomList, error) {
	const stmt = `insert into core.custom_lists (id, tenant_id, name, description, kind, created_at, updated_at) values ($1,$2,$3,$4,$5,$6,$7) returning id, tenant_id, name, description, kind, created_at, updated_at`
	var out platform.CustomList
	err := r.q.QueryRow(ctx, stmt, item.ID, item.TenantID, item.Name, item.Description, item.Kind, item.CreatedAt, item.UpdatedAt).Scan(&out.ID, &out.TenantID, &out.Name, &out.Description, &out.Kind, &out.CreatedAt, &out.UpdatedAt)
	return out, err
}

func (r CustomListRepository) ListLists(ctx context.Context, tenantID string) ([]platform.CustomList, error) {
	const stmt = `select id, tenant_id, name, description, kind, created_at, updated_at from core.custom_lists where tenant_id = $1 order by updated_at desc, created_at desc`
	rows, err := r.q.Query(ctx, stmt, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []platform.CustomList
	for rows.Next() {
		var item platform.CustomList
		if err := rows.Scan(&item.ID, &item.TenantID, &item.Name, &item.Description, &item.Kind, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r CustomListRepository) GetListByID(ctx context.Context, tenantID, listID string) (platform.CustomList, error) {
	const stmt = `select id, tenant_id, name, description, kind, created_at, updated_at from core.custom_lists where tenant_id = $1 and id = $2`
	var out platform.CustomList
	err := r.q.QueryRow(ctx, stmt, tenantID, listID).Scan(&out.ID, &out.TenantID, &out.Name, &out.Description, &out.Kind, &out.CreatedAt, &out.UpdatedAt)
	return out, err
}

func (r CustomListRepository) UpdateList(ctx context.Context, item platform.CustomList) (platform.CustomList, error) {
	const stmt = `update core.custom_lists set name = $3, description = $4, kind = $5, updated_at = $6 where tenant_id = $1 and id = $2 returning id, tenant_id, name, description, kind, created_at, updated_at`
	var out platform.CustomList
	err := r.q.QueryRow(ctx, stmt, item.TenantID, item.ID, item.Name, item.Description, item.Kind, item.UpdatedAt).Scan(&out.ID, &out.TenantID, &out.Name, &out.Description, &out.Kind, &out.CreatedAt, &out.UpdatedAt)
	return out, err
}

func (r CustomListRepository) DeleteList(ctx context.Context, tenantID, listID string) error {
	const deleteEntries = `delete from core.custom_list_entries where tenant_id = $1 and list_id = $2`
	if _, err := r.q.Exec(ctx, deleteEntries, tenantID, listID); err != nil {
		return err
	}
	const deleteList = `delete from core.custom_lists where tenant_id = $1 and id = $2`
	_, err := r.q.Exec(ctx, deleteList, tenantID, listID)
	return err
}

func (r CustomListRepository) Create(ctx context.Context, item platform.CustomListEntry) (platform.CustomListEntry, error) {
	const stmt = `insert into core.custom_list_entries (id, tenant_id, list_id, list_name, value, created_at) values ($1,$2,$3,$4,$5,$6) returning id, tenant_id, list_id, list_name, value, created_at`
	var out platform.CustomListEntry
	err := r.q.QueryRow(ctx, stmt, item.ID, item.TenantID, item.ListID, item.ListName, item.Value, item.CreatedAt).Scan(&out.ID, &out.TenantID, &out.ListID, &out.ListName, &out.Value, &out.CreatedAt)
	return out, err
}

func (r CustomListRepository) ListByName(ctx context.Context, tenantID, listName string) ([]platform.CustomListEntry, error) {
	stmt := `select id, tenant_id, list_id, list_name, value, created_at from core.custom_list_entries where tenant_id = $1 and list_name = $2 order by created_at desc`
	args := []any{tenantID, listName}
	if listName == "" {
		stmt = `select id, tenant_id, list_id, list_name, value, created_at from core.custom_list_entries where tenant_id = $1 order by list_name asc, created_at desc`
		args = []any{tenantID}
	}
	rows, err := r.q.Query(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []platform.CustomListEntry
	for rows.Next() {
		var item platform.CustomListEntry
		if err := rows.Scan(&item.ID, &item.TenantID, &item.ListID, &item.ListName, &item.Value, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r CustomListRepository) ListEntriesByListID(ctx context.Context, tenantID, listID string) ([]platform.CustomListEntry, error) {
	const stmt = `select id, tenant_id, list_id, list_name, value, created_at from core.custom_list_entries where tenant_id = $1 and list_id = $2 order by created_at desc`
	rows, err := r.q.Query(ctx, stmt, tenantID, listID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []platform.CustomListEntry
	for rows.Next() {
		var item platform.CustomListEntry
		if err := rows.Scan(&item.ID, &item.TenantID, &item.ListID, &item.ListName, &item.Value, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r CustomListRepository) UpdateEntry(ctx context.Context, item platform.CustomListEntry) (platform.CustomListEntry, error) {
	const stmt = `update core.custom_list_entries set value = $4 where tenant_id = $1 and list_id = $2 and id = $3 returning id, tenant_id, list_id, list_name, value, created_at`
	var out platform.CustomListEntry
	err := r.q.QueryRow(ctx, stmt, item.TenantID, item.ListID, item.ID, item.Value).Scan(&out.ID, &out.TenantID, &out.ListID, &out.ListName, &out.Value, &out.CreatedAt)
	return out, err
}

func (r CustomListRepository) RenameEntriesByListID(ctx context.Context, tenantID, listID, listName string) error {
	const stmt = `update core.custom_list_entries set list_name = $3 where tenant_id = $1 and list_id = $2`
	_, err := r.q.Exec(ctx, stmt, tenantID, listID, listName)
	return err
}

func (r CustomListRepository) DeleteEntry(ctx context.Context, tenantID, listID, entryID string) error {
	const stmt = `delete from core.custom_list_entries where tenant_id = $1 and list_id = $2 and id = $3`
	_, err := r.q.Exec(ctx, stmt, tenantID, listID, entryID)
	return err
}

func (r CustomListRepository) Contains(ctx context.Context, tenantID, listName, value string) (bool, error) {
	const stmt = `select exists(select 1 from core.custom_list_entries where tenant_id = $1 and list_name = $2 and value = $3)`
	var exists bool
	err := r.q.QueryRow(ctx, stmt, tenantID, listName, value).Scan(&exists)
	return exists, err
}

func (r RecordTagRepository) Create(ctx context.Context, item platform.RecordTag) (platform.RecordTag, error) {
	const stmt = `insert into core.record_tags (id, tenant_id, object_type, object_id, tag, created_at) values ($1,$2,$3,$4,$5,$6) returning id, tenant_id, object_type, object_id, tag, created_at`
	var out platform.RecordTag
	err := r.q.QueryRow(ctx, stmt, item.ID, item.TenantID, item.ObjectType, item.ObjectID, item.Tag, item.CreatedAt).Scan(&out.ID, &out.TenantID, &out.ObjectType, &out.ObjectID, &out.Tag, &out.CreatedAt)
	return out, err
}

func (r RecordTagRepository) ListByObject(ctx context.Context, tenantID, objectType, objectID string) ([]platform.RecordTag, error) {
	const stmt = `select id, tenant_id, object_type, object_id, tag, created_at from core.record_tags where tenant_id = $1 and object_type = $2 and object_id = $3 order by created_at desc`
	rows, err := r.q.Query(ctx, stmt, tenantID, objectType, objectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []platform.RecordTag
	for rows.Next() {
		var item platform.RecordTag
		if err := rows.Scan(&item.ID, &item.TenantID, &item.ObjectType, &item.ObjectID, &item.Tag, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r RecordTagRepository) HasTag(ctx context.Context, tenantID, objectType, objectID, tag string) (bool, error) {
	const stmt = `select exists(select 1 from core.record_tags where tenant_id = $1 and object_type = $2 and object_id = $3 and tag = $4)`
	var exists bool
	err := r.q.QueryRow(ctx, stmt, tenantID, objectType, objectID, tag).Scan(&exists)
	return exists, err
}

func (r RiskSnapshotRepository) Create(ctx context.Context, item platform.RiskSnapshot) (platform.RiskSnapshot, error) {
	const stmt = `insert into core.risk_snapshots (id, tenant_id, object_type, object_id, risk_level, created_at) values ($1,$2,$3,$4,$5,$6) returning id, tenant_id, object_type, object_id, risk_level, created_at`
	var out platform.RiskSnapshot
	err := r.q.QueryRow(ctx, stmt, item.ID, item.TenantID, item.ObjectType, item.ObjectID, item.RiskLevel, item.CreatedAt).Scan(&out.ID, &out.TenantID, &out.ObjectType, &out.ObjectID, &out.RiskLevel, &out.CreatedAt)
	return out, err
}

func (r RiskSnapshotRepository) GetByObject(ctx context.Context, tenantID, objectType, objectID string) (*platform.RiskSnapshot, error) {
	const stmt = `select id, tenant_id, object_type, object_id, risk_level, created_at from core.risk_snapshots where tenant_id = $1 and object_type = $2 and object_id = $3 order by created_at desc limit 1`
	var out platform.RiskSnapshot
	err := r.q.QueryRow(ctx, stmt, tenantID, objectType, objectID).Scan(&out.ID, &out.TenantID, &out.ObjectType, &out.ObjectID, &out.RiskLevel, &out.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (r IPFlagRepository) Create(ctx context.Context, item platform.IPFlag) (platform.IPFlag, error) {
	const stmt = `insert into core.ip_flags (id, tenant_id, ip_address, flag, created_at) values ($1,$2,$3,$4,$5) returning id, tenant_id, ip_address, flag, created_at`
	var out platform.IPFlag
	err := r.q.QueryRow(ctx, stmt, item.ID, item.TenantID, item.IPAddress, item.Flag, item.CreatedAt).Scan(&out.ID, &out.TenantID, &out.IPAddress, &out.Flag, &out.CreatedAt)
	return out, err
}

func (r IPFlagRepository) HasFlag(ctx context.Context, tenantID, ipAddress, flag string) (bool, error) {
	const stmt = `select exists(select 1 from core.ip_flags where tenant_id = $1 and ip_address = $2 and flag = $3)`
	var exists bool
	err := r.q.QueryRow(ctx, stmt, tenantID, ipAddress, flag).Scan(&exists)
	return exists, err
}

func (r IPFlagRepository) ListByIP(ctx context.Context, tenantID, ipAddress string) ([]platform.IPFlag, error) {
	const stmt = `select id, tenant_id, ip_address, flag, created_at from core.ip_flags where tenant_id = $1 and ip_address = $2 order by created_at desc`
	rows, err := r.q.Query(ctx, stmt, tenantID, ipAddress)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []platform.IPFlag
	for rows.Next() {
		var item platform.IPFlag
		if err := rows.Scan(&item.ID, &item.TenantID, &item.IPAddress, &item.Flag, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
