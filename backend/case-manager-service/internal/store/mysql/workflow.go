// Package mysql contains staged case-owner adapters. No runtime selector uses
// these adapters until the complete owner composition and release are verified.
package mysql

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

type WorkflowStore struct {
	db     *sql.DB
	schema string
	ids    ports.IDGenerator
	clock  ports.Clock
}
type workflowMutation struct {
	*WorkflowStore
	tx *sql.Tx
}

var _ service.WorkflowReceiptStore = (*WorkflowStore)(nil)
var _ service.WorkflowReceiptMutation = workflowMutation{}

func NewWorkflowStore(db *sql.DB, database string, ids ports.IDGenerator, clock ports.Clock) (*WorkflowStore, error) {
	schema, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	if db == nil || ids == nil || clock == nil {
		return nil, errors.New("case workflow database, identity generator and clock required")
	}
	return &WorkflowStore{db, schema, ids, clock}, nil
}
func (s *WorkflowStore) WithinWorkflow(ctx context.Context, fn func(service.WorkflowReceiptMutation) error) error {
	p, ok := access.FromContext(ctx)
	if !ok || p.Kind != access.Service || len(p.ServiceTenants) == 0 {
		return casepkg.ErrForbidden
	}
	if fn == nil {
		return errors.New("workflow transaction callback required")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(workflowMutation{s, tx}); err != nil {
		return err
	}
	// An uncertain commit is returned to the caller; the original receipt identity
	// must be reconciled. Never retry a possibly committed business operation here.
	return tx.Commit()
}

func (s *WorkflowStore) Receipt(ctx context.Context, tenant uuid.UUID, source string, id uuid.UUID) (*ports.IntakeReceipt, error) {
	if err := access.Integration(ctx, tenant); err != nil {
		return nil, err
	}
	return readReceipt(ctx, s.db, s.schema, tenant, source, id)
}

type rowQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readReceipt(ctx context.Context, q rowQuery, schema string, tenant uuid.UUID, source string, id uuid.UUID) (*ports.IntakeReceipt, error) {
	r := ports.IntakeReceipt{TenantID: tenant, Source: source, EventID: id}
	var caseID mysqlstore.UUID
	var hash []byte
	err := q.QueryRowContext(ctx, "SELECT payload_hash,case_id,created_at FROM "+schema+".case_manager_intake_receipts WHERE tenant_id=? AND source=? AND event_id=?", mysqlstore.UUID(tenant), []byte(source), mysqlstore.UUID(id)).Scan(&hash, &caseID, &r.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.CaseID = uuid.UUID(caseID)
	r.PayloadHash = hex.EncodeToString(hash)
	return &r, nil
}
func (m workflowMutation) Get(ctx context.Context, tenant uuid.UUID, source string, id uuid.UUID) (*ports.IntakeReceipt, error) {
	if err := access.Integration(ctx, tenant); err != nil {
		return nil, err
	}
	return readReceipt(ctx, m.tx, m.schema, tenant, source, id)
}
func (m workflowMutation) Lock(ctx context.Context, tenant uuid.UUID, scope, key string) error {
	if err := access.Integration(ctx, tenant); err != nil {
		return err
	}
	raw, err := json.Marshal([]string{"case-manager-intake", tenant.String(), scope, key})
	if err != nil {
		return err
	}
	hash := sha256.Sum256(raw)
	// Persistent exact-key lock rows replace transaction advisory locks. A hash
	// collision can only serialize unrelated work, never select another receipt.
	_, err = m.tx.ExecContext(ctx, "INSERT INTO "+m.schema+".case_manager_workflow_locks(tenant_id,lock_hash) VALUES(?,?) ON DUPLICATE KEY UPDATE lock_hash=lock_hash", mysqlstore.UUID(tenant), hash[:])
	return err
}
func (m workflowMutation) Create(ctx context.Context, r ports.IntakeReceipt) error {
	if err := access.Integration(ctx, r.TenantID); err != nil {
		return err
	}
	hash, err := hex.DecodeString(r.PayloadHash)
	if err != nil || len(hash) != 32 || r.CaseID == uuid.Nil || r.EventID == uuid.Nil || len(r.Source) == 0 || len(r.Source) > 100 {
		return casepkg.Invalid("invalid intake receipt")
	}
	_, err = m.tx.ExecContext(ctx, "INSERT INTO "+m.schema+".case_manager_intake_receipts(tenant_id,source,event_id,payload_hash,case_id,created_at) VALUES(?,?,?,?,?,?)", mysqlstore.UUID(r.TenantID), []byte(r.Source), mysqlstore.UUID(r.EventID), hash, mysqlstore.UUID(r.CaseID), r.CreatedAt.UTC())
	return err
}

func (m workflowMutation) ApplyWorkflow(ctx context.Context, in service.WorkflowActionInput) (uuid.UUID, error) {
	if err := access.Integration(ctx, in.TenantID); err != nil {
		return uuid.Nil, err
	}
	if err := service.ValidateWorkflowInput(in); err != nil {
		return uuid.Nil, err
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return uuid.Nil, err
	}
	digest := sha256.Sum256(raw)
	hash := hex.EncodeToString(digest[:])
	if err := m.Lock(ctx, in.TenantID, "workflow", in.WorkflowExecutionID.String()); err != nil {
		return uuid.Nil, err
	}
	prior, err := m.Get(ctx, in.TenantID, "workflow", in.WorkflowExecutionID)
	if err != nil {
		return uuid.Nil, err
	}
	if prior != nil {
		if prior.PayloadHash != hash {
			return uuid.Nil, casepkg.ErrConflict
		}
		return prior.CaseID, nil
	}
	id, err := m.apply(ctx, in)
	if err != nil {
		return uuid.Nil, err
	}
	err = m.Create(ctx, ports.IntakeReceipt{TenantID: in.TenantID, Source: "workflow", EventID: in.WorkflowExecutionID, PayloadHash: hash, CaseID: id, CreatedAt: m.clock.Now()})
	return id, err
}
func (m workflowMutation) activeInbox(ctx context.Context, tenant, id uuid.UUID) error {
	var active bool
	err := m.tx.QueryRowContext(ctx, "SELECT active FROM "+m.schema+".case_manager_inboxes WHERE tenant_id=? AND id=? FOR SHARE", mysqlstore.UUID(tenant), mysqlstore.UUID(id)).Scan(&active)
	if errors.Is(err, sql.ErrNoRows) {
		return casepkg.ErrNotFound
	}
	if err != nil {
		return err
	}
	if !active {
		return casepkg.Invalid("inbox must be active")
	}
	return nil
}
func (m workflowMutation) apply(ctx context.Context, in service.WorkflowActionInput) (uuid.UUID, error) {
	tenant := mysqlstore.UUID(in.TenantID)
	if err := m.activeInbox(ctx, in.TenantID, in.ActionConfig.InboxID); err != nil {
		return uuid.Nil, err
	}
	if err := m.validateTags(ctx, in.TenantID, in.ActionConfig.TagIDs); err != nil {
		return uuid.Nil, err
	}
	var scenario mysqlstore.UUID
	ref := casepkg.DecisionReference{ID: in.DecisionID}
	err := m.tx.QueryRowContext(ctx, "SELECT scenario_id,object_type,object_id FROM "+m.schema+".decision_engine_decisions WHERE tenant_id=? AND id=?", tenant, mysqlstore.UUID(in.DecisionID)).Scan(&scenario, &ref.ObjectType, &ref.ObjectID)
	if errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, casepkg.ErrNotFound
	}
	if err != nil {
		return uuid.Nil, err
	}
	ref.ScenarioID = uuid.UUID(scenario)
	if err := service.ValidateWorkflowDecision(in, ref); err != nil {
		return uuid.Nil, err
	}
	pivot := ref.ObjectType + ":" + ref.ObjectID
	digest := sha256.Sum256([]byte(pivot))
	if err := m.Lock(ctx, in.TenantID, "pivot", pivot); err != nil {
		return uuid.Nil, err
	}
	var existing mysqlstore.UUID
	err = m.tx.QueryRowContext(ctx, "SELECT case_id FROM "+m.schema+".case_manager_case_decisions WHERE tenant_id=? AND decision_id=?", tenant, mysqlstore.UUID(in.DecisionID)).Scan(&existing)
	if err == nil {
		return uuid.UUID(existing), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, err
	}
	var id mysqlstore.UUID
	if in.ActionType != "create_case" {
		query := "SELECT c.id FROM " + m.schema + ".case_manager_case_decisions d FORCE INDEX(case_pivot) JOIN " + m.schema + ".case_manager_cases c ON c.tenant_id=d.tenant_id AND c.id=d.case_id JOIN " + m.schema + ".case_manager_inboxes i ON i.tenant_id=c.tenant_id AND i.id=c.inbox_id WHERE d.tenant_id=? AND d.pivot_hash=? AND d.pivot_value=? AND c.status<>'closed' AND i.active=1"
		args := []any{tenant, digest[:], []byte(pivot)}
		if !in.ActionConfig.AnyInbox {
			query += " AND c.inbox_id=?"
			args = append(args, mysqlstore.UUID(in.ActionConfig.InboxID))
		}
		query += " ORDER BY c.created_at DESC,c.id DESC LIMIT 1 FOR SHARE OF c"
		err = m.tx.QueryRowContext(ctx, query, args...).Scan(&id)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, err
		}
	}
	now := m.clock.Now().UTC()
	if uuid.UUID(id) == uuid.Nil {
		if in.ActionType == "add_to_case" {
			return uuid.Nil, casepkg.ErrNotFound
		}
		name := in.ActionConfig.Name
		if strings.TrimSpace(name) == "" {
			name = in.ActionConfig.Title
		}
		if strings.TrimSpace(name) == "" {
			name = "Workflow case"
		}
		name = strings.TrimSpace(name)
		id = mysqlstore.UUID(m.ids.New())
		_, err = m.tx.ExecContext(ctx, "INSERT INTO "+m.schema+".case_manager_cases(id,tenant_id,inbox_id,name,status,outcome,type,created_at,updated_at) VALUES(?,?,?,?,'pending','unset','decision',?,?)", id, tenant, mysqlstore.UUID(in.ActionConfig.InboxID), name, now, now)
		if err != nil {
			return uuid.Nil, err
		}
		if err := m.event(ctx, in.TenantID, uuid.UUID(id), "case_created", "", "", name); err != nil {
			return uuid.Nil, err
		}
	}
	_, err = m.tx.ExecContext(ctx, "INSERT INTO "+m.schema+".case_manager_case_decisions(id,tenant_id,case_id,decision_id,scenario_id,object_type,object_id,pivot_hash,pivot_value,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)", mysqlstore.UUID(m.ids.New()), tenant, id, mysqlstore.UUID(in.DecisionID), scenario, ref.ObjectType, ref.ObjectID, digest[:], []byte(pivot), now)
	if err != nil {
		return uuid.Nil, err
	}
	if err := m.event(ctx, in.TenantID, uuid.UUID(id), "decision_added", in.DecisionID.String(), "decision", ""); err != nil {
		return uuid.Nil, err
	}
	if err := m.addTags(ctx, in.TenantID, uuid.UUID(id), in.ActionConfig.TagIDs); err != nil {
		return uuid.Nil, err
	}
	return uuid.UUID(id), nil
}

func (m workflowMutation) validateTags(ctx context.Context, tenant uuid.UUID, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	if len(ids) > 500 {
		return casepkg.Invalid("at most 500 workflow tags are allowed")
	}
	unique := make(map[uuid.UUID]bool, len(ids))
	ordered := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil {
			return casepkg.Invalid("tag ID is required")
		}
		if !unique[id] {
			unique[id] = true
			ordered = append(ordered, id)
		}
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].String() < ordered[j].String() })
	args := []any{mysqlstore.UUID(tenant)}
	for _, id := range ordered {
		args = append(args, mysqlstore.UUID(id))
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ordered)), ",")
	rows, err := m.tx.QueryContext(ctx, "SELECT active,target FROM "+m.schema+".case_manager_tags FORCE INDEX(PRIMARY) WHERE tenant_id=? AND id IN ("+marks+") ORDER BY id FOR SHARE", args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var active bool
		var target string
		if err := rows.Scan(&active, &target); err != nil {
			return err
		}
		if !active || target != "case" {
			return casepkg.Invalid("tag must be an active case tag")
		}
		n++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if n != len(ordered) {
		return casepkg.ErrNotFound
	}
	return nil
}
func (m workflowMutation) event(ctx context.Context, tenant, id uuid.UUID, kind, resource, resourceType, value string) error {
	event := casepkg.Event{ID: m.ids.New(), TenantID: tenant, CaseID: id, EventType: kind, ResourceID: resource, ResourceType: resourceType, NewValue: value, CreatedAt: m.clock.Now().UTC()}
	return m.events(ctx, []casepkg.Event{event})
}

func (m workflowMutation) addTags(ctx context.Context, tenant, id uuid.UUID, tags []uuid.UUID) error {
	if len(tags) == 0 {
		return nil
	}
	if len(tags) > 500 {
		return casepkg.Invalid("at most 500 workflow tags are allowed")
	}
	ordered := append([]uuid.UUID(nil), tags...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].String() < ordered[j].String() })
	values := make([]string, 0, len(tags))
	args := make([]any, 0, len(tags)*4)
	now := m.clock.Now().UTC()
	for i, tag := range ordered {
		if i > 0 && tag == ordered[i-1] {
			continue
		}
		values = append(values, "(?,?,?,?)")
		args = append(args, mysqlstore.UUID(tenant), mysqlstore.UUID(id), mysqlstore.UUID(tag), now)
	}
	if _, err := m.tx.ExecContext(ctx, "INSERT INTO "+m.schema+".case_manager_case_tags(tenant_id,case_id,tag_id,created_at) VALUES"+strings.Join(values, ",")+" ON DUPLICATE KEY UPDATE tag_id=tag_id", args...); err != nil {
		return err
	}
	// Keep source audit semantics for repeated tag actions while deduplicating
	// membership writes. Bounded batches avoid a query per tag or notification.
	events := make([]casepkg.Event, 0, len(tags))
	for _, tag := range tags {
		events = append(events, casepkg.Event{ID: m.ids.New(), TenantID: tenant, CaseID: id, EventType: "tags_updated", ResourceID: tag.String(), ResourceType: "case_tag", NewValue: tag.String(), CreatedAt: m.clock.Now().UTC()})
	}
	return m.events(ctx, events)
}
func (m workflowMutation) events(ctx context.Context, events []casepkg.Event) error {
	if len(events) > 500 {
		return casepkg.Invalid("case event batch exceeds limit")
	}
	for start := 0; start < len(events); start += 100 {
		batch := events[start:min(start+100, len(events))]
		args := make([]any, 0, len(batch)*6)
		for _, event := range batch {
			body, err := json.Marshal(event)
			if err != nil {
				return err
			}
			args = append(args, mysqlstore.UUID(event.ID), mysqlstore.UUID(event.TenantID), mysqlstore.UUID(event.CaseID), event.EventType, mysqlstore.JSON(body), event.CreatedAt)
		}
		values := strings.TrimSuffix(strings.Repeat("(?,?,?,?,?,?),", len(batch)), ",")
		if _, err := m.tx.ExecContext(ctx, "INSERT INTO "+m.schema+".case_manager_case_events(id,tenant_id,case_id,event_type,payload,created_at) VALUES"+values, args...); err != nil {
			return err
		}
		values = strings.TrimSuffix(strings.Repeat("(?,?,'case',?,?,?,'pending',?),", len(batch)), ",")
		if _, err := m.tx.ExecContext(ctx, "INSERT INTO "+m.schema+".case_manager_outbox_events(id,tenant_id,aggregate_type,aggregate_id,event_type,payload,status,created_at) VALUES"+values, args...); err != nil {
			return err
		}
	}
	return nil
}
