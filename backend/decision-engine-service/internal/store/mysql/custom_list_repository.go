package mysql

import (
	"context"
	"database/sql"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/platform"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type CustomListRepository struct{ authoringRepository }

var _ ports.CustomListRepository = CustomListRepository{}

func (r CustomListRepository) listSQL() string {
	return "SELECT id,tenant_id,created_at,updated_at," + r.projection("name", "description", "kind") + " FROM " + r.schema + ".decision_engine_custom_lists"
}
func (r CustomListRepository) scanList(row rowScanner) (platform.CustomList, int64, error) {
	var id, tenant mysqlstore.UUID
	var created, updated mysqlstore.DateTime
	var size int64
	var name, description, kind mysqlstore.Text
	if err := row.Scan(&id, &tenant, &created, &updated, &size, &name, &description, &kind); err != nil {
		return platform.CustomList{}, 0, err
	}
	return platform.CustomList{ID: uuid.UUID(id).String(), TenantID: uuid.UUID(tenant).String(), Name: string(name), Description: string(description), Kind: string(kind), CreatedAt: created.Time, UpdatedAt: updated.Time}, size, nil
}
func (r CustomListRepository) GetListByID(ctx context.Context, t, id string) (platform.CustomList, error) {
	args, err := r.ids(t, id)
	if err != nil {
		return platform.CustomList{}, err
	}
	return platformOne(r.platformRepository, r.tx.QueryRowContext(ctx, r.listSQL()+" WHERE tenant_id=? AND id=?", args...), r.scanList)
}
func (r CustomListRepository) ListLists(ctx context.Context, t string) ([]platform.CustomList, error) {
	args, err := r.ids(t)
	if err != nil {
		return nil, err
	}
	return platformList(ctx, r.platformRepository, r.listSQL()+" WHERE tenant_id=? ORDER BY updated_at DESC,created_at DESC,id DESC", args, r.scanList)
}
func (r CustomListRepository) CreateList(ctx context.Context, v platform.CustomList) (platform.CustomList, error) {
	id, err := r.creation(v.TenantID, v.ID, v.CreatedAt, v.Name, v.Description, v.Kind)
	if err != nil {
		return platform.CustomList{}, err
	}
	if _, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".decision_engine_custom_lists(id,tenant_id,name,description,kind,created_at,updated_at) VALUES(?,?,?,?,?,?,?)", id, mysqlstore.UUID(r.tenant), mysqlstore.Text(v.Name), mysqlstore.Text(v.Description), mysqlstore.Text(v.Kind), mysqlstore.DateTime{Time: v.CreatedAt}, mysqlstore.DateTime{Time: v.UpdatedAt}); err != nil {
		return platform.CustomList{}, err
	}
	return r.GetListByID(ctx, v.TenantID, v.ID)
}
func (r CustomListRepository) lockList(ctx context.Context, t, id string) error {
	return r.lockListMode(ctx, t, id, " FOR UPDATE")
}
func (r CustomListRepository) lockListMode(ctx context.Context, t, id, mode string) error {
	if err := r.mutating(); err != nil {
		return err
	}
	args, err := r.ids(t, id)
	if err != nil {
		return err
	}
	var found mysqlstore.UUID
	return r.tx.QueryRowContext(ctx, "SELECT id FROM "+r.schema+".decision_engine_custom_lists WHERE tenant_id=? AND id=?"+mode, args...).Scan(&found)
}
func (r CustomListRepository) UpdateList(ctx context.Context, v platform.CustomList) (platform.CustomList, error) {
	if err := r.scope(v.TenantID, v.Name, v.Description, v.Kind); err != nil {
		return platform.CustomList{}, err
	}
	if err := r.lockList(ctx, v.TenantID, v.ID); err != nil {
		return platform.CustomList{}, err
	}
	args, _ := r.ids(v.TenantID, v.ID)
	if _, err := r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_custom_lists SET name=?,description=?,kind=?,updated_at=? WHERE tenant_id=? AND id=?", append([]any{mysqlstore.Text(v.Name), mysqlstore.Text(v.Description), mysqlstore.Text(v.Kind), mysqlstore.DateTime{Time: v.UpdatedAt}}, args...)...); err != nil {
		return platform.CustomList{}, err
	}
	if err := r.RenameEntriesByListID(ctx, v.TenantID, v.ID, v.Name); err != nil {
		return platform.CustomList{}, err
	}
	return r.GetListByID(ctx, v.TenantID, v.ID)
}
func (r CustomListRepository) DeleteList(ctx context.Context, t, id string) error {
	if err := r.lockList(ctx, t, id); err != nil {
		return err
	}
	args, _ := r.ids(t, id)
	_, err := r.tx.ExecContext(ctx, "DELETE FROM "+r.schema+".decision_engine_custom_lists WHERE tenant_id=? AND id=?", args...)
	return err
}
func (r CustomListRepository) entrySQL() string {
	return "SELECT e.id,e.tenant_id,e.list_id,e.created_at," + r.projection("COALESCE(l.name,e.list_name)", "e.value") + " FROM " + r.schema + ".decision_engine_custom_list_entries e LEFT JOIN " + r.schema + ".decision_engine_custom_lists l ON l.tenant_id=e.tenant_id AND l.id=e.list_id"
}
func (r CustomListRepository) scanEntry(row rowScanner) (platform.CustomListEntry, int64, error) {
	var id, tenant mysqlstore.UUID
	var list mysqlstore.NullUUID
	var at mysqlstore.DateTime
	var size int64
	var name, value mysqlstore.Text
	if err := row.Scan(&id, &tenant, &list, &at, &size, &name, &value); err != nil {
		return platform.CustomListEntry{}, 0, err
	}
	return platform.CustomListEntry{ID: uuid.UUID(id).String(), TenantID: uuid.UUID(tenant).String(), ListID: uuidPointer(list), CreatedAt: at.Time, ListName: string(name), Value: string(value)}, size, nil
}
func (r CustomListRepository) entry(ctx context.Context, t, id string) (platform.CustomListEntry, error) {
	args, err := r.ids(t, id)
	if err != nil {
		return platform.CustomListEntry{}, err
	}
	return platformOne(r.platformRepository, r.tx.QueryRowContext(ctx, r.entrySQL()+" WHERE e.tenant_id=? AND e.id=?", args...), r.scanEntry)
}
func (r CustomListRepository) Create(ctx context.Context, v platform.CustomListEntry) (platform.CustomListEntry, error) {
	id, err := r.creation(v.TenantID, v.ID, v.CreatedAt, v.ListName, v.Value)
	if err != nil {
		return platform.CustomListEntry{}, err
	}
	list, err := nullUUID(v.ListID)
	if err != nil {
		return platform.CustomListEntry{}, err
	}
	if v.ListID != nil {
		if err := r.lockListMode(ctx, v.TenantID, *v.ListID, " FOR SHARE"); err != nil {
			return platform.CustomListEntry{}, err
		}
		owner, err := r.GetListByID(ctx, v.TenantID, *v.ListID)
		if err != nil {
			return platform.CustomListEntry{}, err
		}
		if owner.Name != v.ListName {
			return platform.CustomListEntry{}, ErrOwnerRelationshipConflict
		}
	}
	if _, err := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".decision_engine_custom_list_entries(id,tenant_id,list_id,list_name,value,created_at) VALUES(?,?,?,?,?,?)", id, mysqlstore.UUID(r.tenant), list, mysqlstore.Text(v.ListName), mysqlstore.Text(v.Value), mysqlstore.DateTime{Time: v.CreatedAt}); err != nil {
		return platform.CustomListEntry{}, err
	}
	return r.entry(ctx, v.TenantID, v.ID)
}
func (r CustomListRepository) ListEntriesByListID(ctx context.Context, t, list string) ([]platform.CustomListEntry, error) {
	args, err := r.ids(t, list)
	if err != nil {
		return nil, err
	}
	return platformList(ctx, r.platformRepository, r.entrySQL()+" WHERE e.tenant_id=? AND e.list_id=? ORDER BY e.created_at DESC,e.id DESC", args, r.scanEntry)
}
func (r CustomListRepository) UpdateEntry(ctx context.Context, v platform.CustomListEntry) (platform.CustomListEntry, error) {
	if v.ListID == nil {
		return platform.CustomListEntry{}, ErrTenantScope
	}
	if err := r.lockListMode(ctx, v.TenantID, *v.ListID, " FOR SHARE"); err != nil {
		return platform.CustomListEntry{}, err
	}
	if err := r.scope(v.TenantID, v.Value); err != nil {
		return platform.CustomListEntry{}, err
	}
	args, err := r.ids(v.TenantID, *v.ListID, v.ID)
	if err != nil {
		return platform.CustomListEntry{}, err
	}
	var found mysqlstore.UUID
	if err := r.tx.QueryRowContext(ctx, "SELECT id FROM "+r.schema+".decision_engine_custom_list_entries WHERE tenant_id=? AND list_id=? AND id=? FOR UPDATE", args...).Scan(&found); err != nil {
		return platform.CustomListEntry{}, err
	}
	if _, err := r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_custom_list_entries SET value=? WHERE tenant_id=? AND list_id=? AND id=?", append([]any{mysqlstore.Text(v.Value)}, args...)...); err != nil {
		return platform.CustomListEntry{}, err
	}
	return r.entry(ctx, v.TenantID, v.ID)
}
func (r CustomListRepository) RenameEntriesByListID(ctx context.Context, t, list, name string) error {
	if err := r.lockList(ctx, t, list); err != nil {
		return err
	}
	if err := r.scope(t, name); err != nil {
		return err
	}
	parent, err := r.GetListByID(ctx, t, list)
	if err != nil {
		return err
	}
	if parent.Name != name {
		return ErrOwnerRelationshipConflict
	}
	args, _ := r.ids(t, list)
	_, err = r.tx.ExecContext(ctx, "UPDATE "+r.schema+".decision_engine_custom_list_entries SET list_name=? WHERE tenant_id=? AND list_id=?", append([]any{mysqlstore.Text(name)}, args...)...)
	return err
}
func (r CustomListRepository) DeleteEntry(ctx context.Context, t, list, id string) error {
	if err := r.lockListMode(ctx, t, list, " FOR SHARE"); err != nil {
		return err
	}
	args, err := r.ids(t, list, id)
	if err != nil {
		return err
	}
	result, err := r.tx.ExecContext(ctx, "DELETE FROM "+r.schema+".decision_engine_custom_list_entries WHERE tenant_id=? AND list_id=? AND id=?", args...)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return sql.ErrNoRows
	}
	return err
}

const foldedListMatch = "l.name_fold_sha=UNHEX(SHA2(CAST(LOWER(? COLLATE utf8mb4_0900_as_cs) AS BINARY),256)) AND CAST(LOWER(l.name COLLATE utf8mb4_0900_as_cs) AS BINARY)=CAST(LOWER(? COLLATE utf8mb4_0900_as_cs) AS BINARY)"
const foldedLegacyMatch = "e.list_fold_sha=UNHEX(SHA2(CAST(LOWER(? COLLATE utf8mb4_0900_as_cs) AS BINARY),256)) AND CAST(LOWER(e.list_name COLLATE utf8mb4_0900_as_cs) AS BINARY)=CAST(LOWER(? COLLATE utf8mb4_0900_as_cs) AS BINARY)"

func (r CustomListRepository) ListByName(ctx context.Context, t, name string) ([]platform.CustomListEntry, error) {
	args, err := r.ids(t)
	if err != nil {
		return nil, err
	}
	if err := r.scope(t, name); err != nil {
		return nil, err
	}
	where := " WHERE e.tenant_id=?"
	if name != "" {
		where += " AND ((e.list_id IS NOT NULL AND " + foldedListMatch + ") OR (e.list_id IS NULL AND " + foldedLegacyMatch + "))"
		for range 4 {
			args = append(args, mysqlstore.Text(name))
		}
	}
	order := "e.created_at DESC,e.id DESC"
	if name == "" {
		order = "COALESCE(l.name,e.list_name)," + order
	}
	return platformList(ctx, r.platformRepository, r.entrySQL()+where+" ORDER BY "+order, args, r.scanEntry)
}
func (r CustomListRepository) Contains(ctx context.Context, t, name, value string) (bool, error) {
	args, err := r.ids(t)
	if err != nil {
		return false, err
	}
	if err := r.scope(t, name, value); err != nil {
		return false, err
	}
	args = append(args, mysqlstore.Text(name), mysqlstore.Text(name), mysqlstore.Text(value), mysqlstore.Text(value))
	var found mysqlstore.Bool
	stmt := "SELECT EXISTS(SELECT 1 FROM " + r.schema + ".decision_engine_custom_lists l JOIN " + r.schema + ".decision_engine_custom_list_entries e ON e.tenant_id=l.tenant_id AND e.list_id=l.id WHERE l.tenant_id=? AND " + foldedListMatch + " AND e.value_sha=UNHEX(SHA2(CAST(? AS BINARY),256)) AND CAST(e.value AS BINARY)=CAST(? AS BINARY))"
	if err := r.tx.QueryRowContext(ctx, stmt, args...).Scan(&found); err != nil {
		return false, err
	}
	if found {
		return true, nil
	}
	stmt = "SELECT EXISTS(SELECT 1 FROM " + r.schema + ".decision_engine_custom_list_entries e WHERE e.tenant_id=? AND e.list_id IS NULL AND " + foldedLegacyMatch + " AND e.value_sha=UNHEX(SHA2(CAST(? AS BINARY),256)) AND CAST(e.value AS BINARY)=CAST(? AS BINARY))"
	if err := r.tx.QueryRowContext(ctx, stmt, args...).Scan(&found); err != nil {
		return false, err
	}
	return bool(found), nil
}
