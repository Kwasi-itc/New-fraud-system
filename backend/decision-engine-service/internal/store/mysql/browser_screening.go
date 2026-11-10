package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// Browser screening uses verified sessions and the same inbox contracts as
// workflow editing. Provider-specific configurations remain read-only.
func (s *BrowserWorkflowAdmin) ScreeningPage(ctx context.Context, parent uuid.UUID) (screening.AuthoringPage, error) {
	out := screening.AuthoringPage{Rules: []screening.AuthoringRule{}, Fields: []string{}, IgnoreLists: []screening.IgnoreListOption{}}
	if s == nil || s.owner == nil || s.copyReader == nil {
		return out, ErrOwnerContractUnavailable
	}
	model, err := s.copyReader.ResolveAuthorizedPreparationModel(ctx)
	if err != nil {
		return out, err
	}
	c := s.owner.configuration
	err = mysqlstore.Snapshot(ctx, c.db, func(tx *sql.Tx) error {
		if err := s.owner.authorize(ctx, tx, c.tenant, AuthoringWorkflowRead); err != nil {
			return err
		}
		r := authoringRepository{platformRepository: platformRepository{tx, c.schema, c.tenant, c.maxRows, c.maxBytes, false}}
		scenario, err := (ScenarioRepository{r}).GetByID(ctx, c.tenant.String(), parent.String())
		if err != nil {
			return err
		}
		out.ScenarioName = scenario.Name
		table, ok := model.Tables[scenario.TriggerObjectType]
		if !ok || table.Archived {
			return ErrPublishedModelUnavailable
		}
		for name, field := range table.Fields {
			if !field.Archived && (field.Type == "String" || field.Type == "string") {
				out.Fields = append(out.Fields, name)
			}
		}
		sort.Strings(out.Fields)
		values, err := screeningConfigRepo(r).ListByScenario(ctx, c.tenant.String(), parent.String())
		if err != nil {
			return err
		}
		inboxSet := map[uuid.UUID]bool{}
		for _, v := range values {
			if v.ReviewInboxID != nil {
				inboxSet[uuid.MustParse(*v.ReviewInboxID)] = true
			}
		}
		visible, err := s.owner.inboxes.Visible(ctx, tx, c.tenant, sortedWorkflowIDs(inboxSet))
		if err != nil {
			return err
		}
		for _, v := range values {
			rule, editable := screening.DecodeAuthoring(v)
			if !editable {
				out.OtherRules++
				continue
			}
			if visible[uuid.MustParse(rule.InboxID)] {
				out.Rules = append(out.Rules, rule)
			}
		}
		return nil
	})
	return out, err
}

// Local matching does not advertise external-provider or recognition features.
// The expected version is compared after locking the row, in the mutation
// transaction. A preview only determines which inbox permissions must be held.
func (s *BrowserWorkflowAdmin) SaveScreening(ctx context.Context, parent uuid.UUID, in screening.AuthoringRule) (screening.AuthoringRule, error) {
	if err := in.Validate(); err != nil {
		return screening.AuthoringRule{}, errors.Join(ErrAdministrationInput, err)
	}
	if in.UseNER || in.IgnoreListID != "" || in.Threshold != nil || in.ResultLimit != nil || len(in.Datasets) > 0 || in.AddressField != "" || in.BirthDateField != "" || in.NationalityField != "" || in.CountryField != "" || in.RegistrationField != "" {
		return screening.AuthoringRule{}, screening.ErrMatchingUnavailable
	}
	page, err := s.ScreeningPage(ctx, parent)
	if err != nil {
		return screening.AuthoringRule{}, err
	}
	found := false
	for _, name := range page.Fields {
		if name == in.NameField {
			found = true
		}
	}
	if !found {
		return screening.AuthoringRule{}, ErrAdministrationInput
	}
	tenant, actor, err := s.binding()
	if err != nil {
		return screening.AuthoringRule{}, err
	}
	var result screening.AuthoringRule
	err = s.owner.mutate(ctx, tenant, parent.String(), actor, func(r authoringRepository) error {
		repo := screeningConfigRepo(r)
		preview, pe := repo.GetByID(ctx, tenant, parent.String(), in.ID)
		if pe != nil && !errors.Is(pe, sql.ErrNoRows) {
			return pe
		}
		inboxes := map[uuid.UUID]bool{uuid.MustParse(in.InboxID): true}
		if pe == nil {
			if preview.ReviewInboxID == nil {
				return screening.ErrAuthoringConflict
			}
			inboxes[uuid.MustParse(*preview.ReviewInboxID)] = true
		}
		if err := s.owner.inboxes.AuthorizeBatch(ctx, r.tx, r.tenant, sortedWorkflowIDs(inboxes), true, false); err != nil {
			return err
		}
		if err := s.owner.inboxes.Authorize(ctx, r.tx, r.tenant, uuid.MustParse(in.InboxID), true, in.Enabled); err != nil {
			return err
		}
		// Serializes browser creates; existing rows are also locked against native
		// configuration writers before reloading their current values.
		if err := r.lockWorkflowAuthoring(ctx, tenant, parent.String()); err != nil {
			return err
		}
		le := repo.configurationRepository.lock(ctx, tenant, parent.String(), in.ID)
		creating := errors.Is(le, sql.ErrNoRows)
		if le != nil && !creating {
			return le
		}
		current := screening.Config{}
		if !creating {
			current, err = repo.GetByID(ctx, tenant, parent.String(), in.ID)
			if err != nil {
				return err
			}
		}
		if creating != errors.Is(pe, sql.ErrNoRows) || !creating && !current.UpdatedAt.Equal(preview.UpdatedAt) {
			return screening.ErrAuthoringConflict
		}
		if creating {
			if in.UpdatedAt != nil {
				return screening.ErrAuthoringConflict
			}
		} else {
			if _, ok := screening.DecodeAuthoring(current); !ok {
				return screening.ErrAuthoringConflict
			}
			if in.UpdatedAt == nil || !in.UpdatedAt.Equal(current.UpdatedAt) {
				return screening.ErrAuthoringConflict
			}
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		if !creating && !now.After(current.UpdatedAt) {
			now = current.UpdatedAt.Add(time.Microsecond)
		}
		created := now
		if !creating {
			created = current.CreatedAt
		}
		value := screening.Config{ID: in.ID, TenantID: tenant, ScenarioID: parent.String(), Name: in.Name, Provider: "opensanctions", ConfigJSON: in.ConfigJSON(), AllowedOutcomes: in.Outcomes, ReviewInboxID: &in.InboxID, Active: in.Enabled, CreatedAt: created, UpdatedAt: now}
		if creating {
			value, err = repo.Create(ctx, value)
		} else {
			value, err = repo.Update(ctx, value)
		}
		if err != nil {
			return err
		}
		result, _ = screening.DecodeAuthoring(value)
		return nil
	})
	return result, err
}

type BrowserScreeningSummary struct {
	ID       string         `json:"id"`
	ConfigID string         `json:"config_id"`
	Status   string         `json:"status"`
	Request  map[string]any `json:"request_json"`
	Response map[string]any `json:"response_json"`
}

// A summary never exposes queries, raw provider responses, or operational errors.
// Linked case inboxes and config inboxes are checked in the same snapshot.
func (s *BrowserWorkflowAdmin) ScreeningSummaries(ctx context.Context, decision uuid.UUID) ([]BrowserScreeningSummary, error) {
	if s == nil || s.owner == nil {
		return nil, ErrOwnerContractUnavailable
	}
	c := s.owner.configuration
	out := []BrowserScreeningSummary{}
	err := mysqlstore.Snapshot(ctx, c.db, func(tx *sql.Tx) error {
		if err := s.owner.authorize(ctx, tx, c.tenant, AuthoringWorkflowRead); err != nil {
			return err
		}
		r := authoringRepository{platformRepository: platformRepository{tx, c.schema, c.tenant, c.maxRows, c.maxBytes, false}}
		d, err := (DecisionRepository{r}).GetByID(ctx, c.tenant.String(), decision.String())
		if err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, "SELECT c.inbox_id FROM "+c.schema+".case_manager_case_decisions cd JOIN "+c.schema+".case_manager_cases c ON c.tenant_id=cd.tenant_id AND c.id=cd.case_id WHERE cd.tenant_id=? AND cd.decision_id=? ORDER BY cd.case_id LIMIT 251", mysqlstore.UUID(c.tenant), mysqlstore.UUID(decision))
		if err != nil {
			return err
		}
		inboxes := []uuid.UUID{}
		for rows.Next() {
			var id mysqlstore.UUID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			inboxes = append(inboxes, uuid.UUID(id))
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return err
		}
		if len(inboxes) > 250 {
			return ErrProjectionLimit
		}
		unique := map[uuid.UUID]bool{}
		for _, id := range inboxes {
			unique[id] = true
		}
		if err := s.owner.inboxes.AuthorizeBatch(ctx, tx, c.tenant, sortedWorkflowIDs(unique), false, false); err != nil {
			return err
		}
		repo := ScreeningExecutionRepository{providerExecutionRepository: providerExecutionRepository{authoringRepository: r, screening: true}}
		executions, err := repo.ListByDecision(ctx, c.tenant.String(), decision.String())
		if err != nil {
			return err
		}
		for _, e := range executions {
			if e.ScenarioID != d.ScenarioID {
				return ErrOwnerRelationshipConflict
			}
			config, err := screeningConfigRepo(r).GetByID(ctx, c.tenant.String(), e.ScenarioID, e.ConfigID)
			if err != nil {
				return err
			}
			if config.ReviewInboxID == nil {
				return ErrDecisionAuthorizationDenied
			}
			if err := s.owner.inboxes.Authorize(ctx, tx, c.tenant, uuid.MustParse(*config.ReviewInboxID), false, false); err != nil {
				return err
			}
			response := map[string]any{}
			var raw map[string]any
			if json.Unmarshal(e.ResponseJSON, &raw) == nil {
				for _, k := range []string{"status", "partial", "match_count"} {
					v := raw[k]
					switch v.(type) {
					case string, bool, float64:
						response[k] = v
					}
				}
			}
			out = append(out, BrowserScreeningSummary{e.ID, e.ConfigID, string(e.Status), map[string]any{"provider": config.Provider}, response})
		}
		return nil
	})
	return out, err
}
