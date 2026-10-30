package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ScreeningAuthoringRepository struct{ DB *pgxpool.Pool }

func screeningNameFields(ctx context.Context, q queryable, t, sc string) ([]string, error) {
	rows, err := q.Query(ctx, `SELECT f.name FROM core.scenarios s JOIN core.model_tables mt ON mt.tenant_id=s.tenant_id AND mt.name=s.trigger_object_type AND NOT mt.archived JOIN core.model_fields f ON f.tenant_id=mt.tenant_id AND f.table_id=mt.id AND NOT f.archived WHERE s.tenant_id=$1 AND s.id=$2 AND f.data_type='string' ORDER BY f.name LIMIT 501`, t, sc)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	if len(out) > 500 {
		return nil, screening.ErrAuthoringConflict
	}
	return out, rows.Err()
}

func (r ScreeningAuthoringRepository) List(ctx context.Context, t, sc string, a workflow.AuthoringActor) (screening.AuthoringPage, error) {
	out := screening.AuthoringPage{Rules: []screening.AuthoringRule{}}
	if err := authorizeWorkflow(ctx, r.DB, t, a, false); err != nil {
		return out, err
	}
	if err := r.DB.QueryRow(ctx, `SELECT name FROM core.scenarios WHERE tenant_id=$1 AND id=$2`, t, sc).Scan(&out.ScenarioName); err != nil {
		return out, err
	}
	var err error
	listRows, listErr := r.DB.Query(ctx, `SELECT id,name FROM core.custom_lists WHERE tenant_id=$1 ORDER BY name,id LIMIT 101`, t)
	if listErr != nil {
		return out, listErr
	}
	out.IgnoreLists = []screening.IgnoreListOption{}
	for listRows.Next() {
		var option screening.IgnoreListOption
		if err := listRows.Scan(&option.ID, &option.Name); err != nil {
			listRows.Close()
			return out, err
		}
		out.IgnoreLists = append(out.IgnoreLists, option)
	}
	listErr = listRows.Err()
	listRows.Close()
	if listErr != nil {
		return out, listErr
	}
	if len(out.IgnoreLists) > 100 {
		return out, screening.ErrAuthoringConflict
	}
	out.Fields, err = screeningNameFields(ctx, r.DB, t, sc)
	if err != nil {
		return out, err
	}
	// Bounded config listing with inbox visibility in the same query, no N+1 access checks.
	rows, err := r.DB.Query(ctx, `SELECT c.id,c.name,c.allowed_outcomes,c.provider,c.config_json,c.active,c.updated_at,c.review_inbox_id,
	EXISTS(SELECT 1 FROM case_manager.inboxes i WHERE i.tenant_id=c.tenant_id AND i.id=c.review_inbox_id AND (identity.authorize_case_v1($3,$1,$4,$5,true,false) OR EXISTS(SELECT 1 FROM case_manager.inbox_users u WHERE u.tenant_id=i.tenant_id AND u.inbox_id=i.id AND u.user_id=$3)))
	FROM core.screening_configs c WHERE c.tenant_id=$1 AND c.scenario_id=$2 ORDER BY c.created_at,c.id LIMIT 101`, t, sc, a.UserID, a.SessionID, a.Version)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var c screening.Config
		var visible bool
		if err = rows.Scan(&c.ID, &c.Name, &c.AllowedOutcomes, &c.Provider, &c.ConfigJSON, &c.Active, &c.UpdatedAt, &c.ReviewInboxID, &visible); err != nil {
			return out, err
		}
		n++
		if n > 100 {
			return out, screening.ErrAuthoringConflict
		}
		if !visible {
			continue
		}
		if rule, ok := screening.DecodeAuthoring(c); ok {
			out.Rules = append(out.Rules, rule)
		} else {
			out.OtherRules++
		}
	}
	return out, rows.Err()
}

func (r ScreeningAuthoringRepository) Save(ctx context.Context, t, sc string, a workflow.AuthoringActor, in screening.AuthoringRule) (screening.AuthoringRule, error) {
	empty := screening.AuthoringRule{}
	if err := in.Validate(); err != nil {
		return empty, err
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(context.Background())
	if err = authorizeWorkflow(ctx, tx, t, a, true); err != nil {
		return empty, err
	}
	// Configs execute outside this authoring transaction. Serialize edits only per scenario.
	var scenario string
	if err = tx.QueryRow(ctx, `SELECT id FROM core.scenarios WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, t, sc).Scan(&scenario); err != nil {
		return empty, err
	}
	if err = workflowInbox(ctx, tx, t, in.InboxID, a, true, in.Enabled); err != nil {
		return empty, err
	}
	if in.IgnoreListID != "" {
		var list string
		if err := tx.QueryRow(ctx, `SELECT id FROM core.custom_lists WHERE tenant_id=$1 AND id=$2 FOR SHARE`, t, in.IgnoreListID).Scan(&list); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return empty, screening.ErrAuthoringConflict
			}
			return empty, err
		}
		if _, err := NewCustomListRepository(tx).ScreeningIgnoreWords(ctx, t, in.IgnoreListID); err != nil {
			return empty, screening.ErrAuthoringConflict
		}
	}
	for _, mapped := range []string{in.NameField, in.AddressField, in.BirthDateField, in.NationalityField, in.CountryField, in.RegistrationField} {
		if mapped == "" {
			continue
		}
		var field string
		err = tx.QueryRow(ctx, `SELECT f.name FROM core.scenarios s JOIN core.model_tables mt ON mt.tenant_id=s.tenant_id AND mt.name=s.trigger_object_type AND NOT mt.archived JOIN core.model_fields f ON f.tenant_id=mt.tenant_id AND f.table_id=mt.id AND NOT f.archived WHERE s.tenant_id=$1 AND s.id=$2 AND f.name=$3 AND f.data_type='string' FOR SHARE OF mt,f`, t, sc, mapped).Scan(&field)
		if errors.Is(err, pgx.ErrNoRows) {
			return empty, screening.ErrAuthoringConflict
		}
		if err != nil {
			return empty, err
		}
	}
	repo := NewScreeningConfigRepository(tx)
	var locked string
	err = tx.QueryRow(ctx, `SELECT id FROM core.screening_configs WHERE tenant_id=$1 AND scenario_id=$2 AND id=$3 FOR UPDATE`, t, sc, in.ID).Scan(&locked)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return empty, err
	}
	old, err := repo.GetByID(ctx, t, sc, in.ID)
	create := errors.Is(err, pgx.ErrNoRows)
	if err != nil && !create {
		return empty, err
	}
	if create && in.UpdatedAt != nil {
		return empty, screening.ErrAuthoringConflict
	}
	if !create {
		_, ok := screening.DecodeAuthoring(old)
		if !ok || in.UpdatedAt == nil || !in.UpdatedAt.Equal(old.UpdatedAt) {
			return empty, screening.ErrAuthoringConflict
		}
		if err = workflowInbox(ctx, tx, t, *old.ReviewInboxID, a, true, false); err != nil {
			return empty, err
		}
	}
	var count int
	if create {
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM core.screening_configs WHERE tenant_id=$1 AND scenario_id=$2`, t, sc).Scan(&count); err != nil {
			return empty, err
		}
		if count >= 100 {
			return empty, screening.ErrAuthoringConflict
		}
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if !create && !now.After(old.UpdatedAt) {
		now = old.UpdatedAt.Add(time.Microsecond)
	}
	item := screening.Config{ID: in.ID, TenantID: t, ScenarioID: sc, Name: in.Name, AllowedOutcomes: in.Outcomes, Provider: "opensanctions", ConfigJSON: in.ConfigJSON(), Active: in.Enabled, ReviewInboxID: &in.InboxID, CreatedAt: now, UpdatedAt: now}
	var saved screening.Config
	if create {
		saved, err = repo.Create(ctx, item)
	} else {
		saved, err = repo.Update(ctx, item)
	}
	if err != nil {
		return empty, err
	}
	result, _ := screening.DecodeAuthoring(saved)
	raw, err := json.Marshal(result)
	if err != nil {
		return empty, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO core.screening_authoring_events(id,tenant_id,scenario_id,config_id,actor_id,snapshot,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, uuid.NewString(), t, sc, in.ID, a.UserID, raw, now); err != nil {
		return empty, err
	}
	if err = tx.Commit(ctx); err != nil {
		return empty, err
	}
	return result, nil
}
