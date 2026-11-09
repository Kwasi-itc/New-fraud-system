package mysql

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/platform"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// CustomListAdmin composes the existing list owner, including its separate
// read/mutation authority. Callback results are released only after commit.
type CustomListAdmin struct {
	owner *CustomListStore
	ids   ports.IDGenerator
	clock ports.Clock
}

func NewCustomListAdmin(owner *CustomListStore, ids ports.IDGenerator, clock ports.Clock) (*CustomListAdmin, error) {
	if owner == nil || owner.configuration == nil || ids == nil || clock == nil {
		return nil, ErrOwnerContractUnavailable
	}
	return &CustomListAdmin{owner, ids, clock}, nil
}
func (s *CustomListAdmin) MatchesExecutionStore(e *ExecutionStore) bool {
	return s != nil && s.owner != nil && s.owner.configuration != nil && (&DecisionStore{s.owner.configuration.configuration}).MatchesExecutionStore(e)
}
func listAdminRun[T any](ctx context.Context, s *CustomListAdmin, write bool, fn func(CustomListRepository) (T, error)) (out T, err error) {
	if s == nil || s.owner == nil {
		return out, ErrOwnerContractUnavailable
	}
	run := s.owner.ReadSnapshot
	if write {
		run = s.owner.Mutate
	}
	err = run(ctx, func(r CustomListRepository) error { out, err = fn(r); return err })
	if err != nil {
		var zero T
		return zero, err
	}
	return out, nil
}
func (s *CustomListAdmin) now() time.Time { return s.clock.Now().UTC().Truncate(time.Microsecond) }
func (s *CustomListAdmin) newID() (string, error) {
	id := s.ids.New()
	if id == uuid.Nil {
		return "", ErrTenantScope
	}
	return id.String(), nil
}
func listAdminText(r CustomListRepository, values ...string) error {
	if e := r.scope(r.tenant.String(), values...); e != nil {
		return errors.Join(ErrAdministrationInput, e)
	}
	return nil
}
func (s *CustomListAdmin) Lists(ctx context.Context) ([]platform.CustomList, error) {
	return listAdminRun(ctx, s, false, func(r CustomListRepository) ([]platform.CustomList, error) {
		return r.ListLists(ctx, r.tenant.String())
	})
}
func (s *CustomListAdmin) EntriesByName(ctx context.Context, name string) ([]platform.CustomListEntry, error) {
	return listAdminRun(ctx, s, false, func(r CustomListRepository) ([]platform.CustomListEntry, error) {
		if e := listAdminText(r, name); e != nil {
			return nil, e
		}
		return r.ListByName(ctx, r.tenant.String(), name)
	})
}
func (s *CustomListAdmin) Get(ctx context.Context, id uuid.UUID) (platform.CustomList, error) {
	return listAdminRun(ctx, s, false, func(r CustomListRepository) (platform.CustomList, error) {
		return r.GetListByID(ctx, r.tenant.String(), id.String())
	})
}
func (s *CustomListAdmin) Save(ctx context.Context, id uuid.UUID, fields platform.CustomList) (platform.CustomList, error) {
	return listAdminRun(ctx, s, true, func(r CustomListRepository) (platform.CustomList, error) {
		var v platform.CustomList
		t := r.tenant.String()
		create := id == uuid.Nil
		if e := listAdminText(r, fields.Name, fields.Description, fields.Kind); e != nil {
			return v, e
		}
		if create {
			key, e := s.newID()
			if e != nil {
				return v, e
			}
			v = platform.CustomList{ID: key, TenantID: t, CreatedAt: s.now()}
		} else {
			if e := r.lockList(ctx, t, id.String()); e != nil {
				return v, e
			}
			var e error
			v, e = r.GetListByID(ctx, t, id.String())
			if e != nil {
				return v, e
			}
		}
		v.Name, v.Description, v.Kind = fields.Name, fields.Description, fields.Kind
		v.UpdatedAt = s.now()
		if create {
			return r.CreateList(ctx, v)
		}
		return r.UpdateList(ctx, v)
	})
}
func (s *CustomListAdmin) Delete(ctx context.Context, id uuid.UUID) error {
	_, e := listAdminRun(ctx, s, true, func(r CustomListRepository) (struct{}, error) {
		return struct{}{}, r.DeleteList(ctx, r.tenant.String(), id.String())
	})
	return e
}
func (s *CustomListAdmin) Entries(ctx context.Context, list uuid.UUID) ([]platform.CustomListEntry, error) {
	return listAdminRun(ctx, s, false, func(r CustomListRepository) ([]platform.CustomListEntry, error) {
		t := r.tenant.String()
		if _, e := r.GetListByID(ctx, t, list.String()); e != nil {
			return nil, e
		}
		return r.ListEntriesByListID(ctx, t, list.String())
	})
}
func (s *CustomListAdmin) SaveEntry(ctx context.Context, list, id uuid.UUID, value string) (platform.CustomListEntry, error) {
	return listAdminRun(ctx, s, true, func(r CustomListRepository) (platform.CustomListEntry, error) {
		var v platform.CustomListEntry
		t, p := r.tenant.String(), list.String()
		if e := listAdminText(r, value); e != nil {
			return v, e
		}
		if e := r.lockListMode(ctx, t, p, " FOR SHARE"); e != nil {
			return v, e
		}
		parent, e := r.GetListByID(ctx, t, p)
		if e != nil {
			return v, e
		}
		v = platform.CustomListEntry{TenantID: t, ListID: &p, ListName: parent.Name, Value: value}
		if id != uuid.Nil {
			v.ID = id.String()
			return r.UpdateEntry(ctx, v)
		}
		v.ID, e = s.newID()
		if e != nil {
			return v, e
		}
		v.CreatedAt = s.now()
		return r.Create(ctx, v)
	})
}
func (s *CustomListAdmin) DeleteEntry(ctx context.Context, list, id uuid.UUID) error {
	_, e := listAdminRun(ctx, s, true, func(r CustomListRepository) (struct{}, error) {
		return struct{}{}, r.DeleteEntry(ctx, r.tenant.String(), list.String(), id.String())
	})
	return e
}

// Import trims/deduplicates within this request, matching source semantics.
// Existing equal values remain allowed. One shared parent lock pins its name;
// inserts are bounded batches, never one lookup/insert/readback per value.
func (s *CustomListAdmin) Import(ctx context.Context, list uuid.UUID, values []string) (int, error) {
	return listAdminRun(ctx, s, true, func(r CustomListRepository) (int, error) {
		if len(values) > r.maxRows {
			return 0, ErrProjectionLimit
		}
		filtered := make([]string, 0, len(values))
		seen := map[string]bool{}
		inputBytes := 0
		for _, raw := range values {
			if len(raw) > r.maxBytes-inputBytes {
				return 0, ErrProjectionLimit
			}
			inputBytes += len(raw)
			if e := listAdminText(r, raw); e != nil {
				return 0, e
			}
			v := strings.TrimSpace(raw)
			if v != "" && !seen[v] {
				seen[v] = true
				filtered = append(filtered, v)
			}
		}
		t, p := r.tenant.String(), list.String()
		if e := r.lockListMode(ctx, t, p, " FOR SHARE"); e != nil {
			return 0, e
		}
		parent, e := r.GetListByID(ctx, t, p)
		if e != nil {
			return 0, e
		}
		at := s.now()
		if _, e := (mysqlstore.DateTime{Time: at}).Value(); e != nil {
			return 0, e
		}
		ids := make([]mysqlstore.UUID, len(filtered))
		used := 0
		for i, value := range filtered {
			size := 128 + len(parent.Name) + len(value)
			if size > r.maxBytes-used {
				return 0, ErrProjectionLimit
			}
			used += size
			id, e := s.newID()
			if e != nil {
				return 0, e
			}
			ids[i] = mysqlstore.UUID(uuid.MustParse(id))
		}
		for start := 0; start < len(filtered); start += 100 {
			end := min(start+100, len(filtered))
			marks := make([]string, 0, end-start)
			args := make([]any, 0, (end-start)*6)
			for i := start; i < end; i++ {
				marks = append(marks, "(?,?,?,?,?,?)")
				args = append(args, ids[i], mysqlstore.UUID(r.tenant), mysqlstore.UUID(list), mysqlstore.Text(parent.Name), mysqlstore.Text(filtered[i]), mysqlstore.DateTime{Time: at})
			}
			if _, e := r.tx.ExecContext(ctx, "INSERT INTO "+r.schema+".decision_engine_custom_list_entries(id,tenant_id,list_id,list_name,value,created_at) VALUES "+strings.Join(marks, ","), args...); e != nil {
				return 0, e
			}
		}
		return len(filtered), nil
	})
}
