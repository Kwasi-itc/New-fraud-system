package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	"github.com/google/uuid"

	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/ports"
)

type CaseService struct {
	analytics    ports.AnalyticsRepository
	reports      ports.ReportRepository
	uploads      ports.UploadRepository
	workspace    ports.WorkspaceRepository
	intake       ports.IntakeRepository
	memberships  ports.MembershipRepository
	contributors ports.ContributorRepository
	references   ports.ReferenceRepository
	decisionRefs map[uuid.UUID]casepkg.DecisionReference
	uow          ports.UnitOfWork
	ids          ports.IDGenerator
	clock        ports.Clock
	inboxes      ports.InboxRepository
	cases        ports.CaseRepository
	decisions    ports.DecisionLinkRepository
	screenings   ports.ScreeningLinkRepository
	tags         ports.TagRepository
	events       ports.EventRepository
	files        ports.FileRepository
}

func NewCaseService(
	ids ports.IDGenerator,
	clock ports.Clock,
	repos ports.Repositories,
	uow ports.UnitOfWork,
) CaseService {
	if uow == nil {
		panic("case service requires a unit of work")
	}
	s := CaseService{ids: ids, clock: clock, uow: uow}
	return s.withRepositories(repos)
}

func (s CaseService) withRepositories(r ports.Repositories) CaseService {
	s.analytics = r.Analytics
	s.reports = r.Reports
	s.uploads = r.Uploads
	s.workspace = r.Workspace
	s.intake = r.Intake
	s.memberships, s.contributors, s.references = r.Memberships, r.Contributors, r.References
	s.inboxes, s.cases, s.decisions, s.screenings = r.Inboxes, r.Cases, r.Decisions, r.Screenings
	s.tags, s.events, s.files = r.Tags, r.Events, r.Files
	return s
}

func transact[T any](ctx context.Context, s CaseService, fn func(CaseService) (T, error)) (T, error) {
	// Nested operations reuse the same transaction-scoped service.
	if s.uow == nil {
		return fn(s)
	}
	var result T
	err := s.uow.WithinTransaction(ctx, func(r ports.Repositories) error {
		txService := s.withRepositories(r)
		txService.uow = nil
		txService.decisionRefs = make(map[uuid.UUID]casepkg.DecisionReference)
		var err error
		result, err = fn(txService)
		return err
	})
	if err != nil {
		var zero T
		return zero, err
	}
	return result, nil
}

type CreateInboxInput struct {
	TenantID          uuid.UUID
	Name              string
	EscalationInboxID *uuid.UUID
}

func (s CaseService) createInbox(ctx context.Context, in CreateInboxInput) (casepkg.Inbox, error) {
	if err := requireIDs(in.TenantID); err != nil {
		return casepkg.Inbox{}, err
	}
	if in.EscalationInboxID != nil {
		if _, err := s.activeInbox(ctx, in.TenantID, *in.EscalationInboxID); err != nil {
			return casepkg.Inbox{}, err
		}
	}
	now := s.clock.Now()
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return casepkg.Inbox{}, casepkg.Invalid("name is required")
	}
	return s.inboxes.Create(ctx, casepkg.Inbox{
		ID:                s.ids.New(),
		TenantID:          in.TenantID,
		Name:              name,
		Status:            "active",
		EscalationInboxID: in.EscalationInboxID,
		CreatedAt:         now,
		UpdatedAt:         now,
	})
}

func (s CaseService) ListInboxes(ctx context.Context, tenantID uuid.UUID) ([]casepkg.Inbox, error) {
	if err := requireIDs(tenantID); err != nil {
		return nil, err
	}
	p, err := access.Tenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	user := ""
	if p.Kind == access.User && !p.Admin {
		user = p.Subject
	}
	return s.inboxes.List(ctx, tenantID, user)
}

func (s CaseService) GetInbox(ctx context.Context, tenantID, inboxID uuid.UUID) (casepkg.Inbox, error) {
	if err := requireIDs(tenantID, inboxID); err != nil {
		return casepkg.Inbox{}, err
	}
	if err := s.inboxAccess(ctx, tenantID, inboxID); err != nil {
		return casepkg.Inbox{}, err
	}
	return s.inboxes.Get(ctx, tenantID, inboxID)
}

type UpdateInboxInput struct {
	SLADays                 *int
	ClearSLA                bool
	TenantID                uuid.UUID
	InboxID                 uuid.UUID
	Name                    *string
	Status                  *string
	EscalationInboxID       *uuid.UUID
	ClearEscalationInbox    bool
	AutoAssignEnabled       *bool
	CaseReviewManual        *bool
	CaseReviewOnCaseCreated *bool
	CaseReviewOnEscalate    *bool
}

func (s CaseService) updateInbox(ctx context.Context, in UpdateInboxInput) (casepkg.Inbox, error) {
	if in.SLADays != nil && (*in.SLADays < 1 || *in.SLADays > 3650 || in.ClearSLA) {
		return casepkg.Inbox{}, casepkg.Invalid("SLA must be 1..3650 days or null")
	}
	if err := requireIDs(in.TenantID, in.InboxID); err != nil {
		return casepkg.Inbox{}, err
	}
	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		return casepkg.Inbox{}, casepkg.Invalid("name is required")
	}
	if in.Status != nil && *in.Status != "active" && *in.Status != "archived" {
		return casepkg.Inbox{}, casepkg.Invalid("invalid inbox status")
	}
	if in.ClearEscalationInbox && in.EscalationInboxID != nil {
		return casepkg.Inbox{}, casepkg.Invalid("conflicting escalation settings")
	}
	if in.EscalationInboxID != nil {
		if *in.EscalationInboxID == in.InboxID {
			return casepkg.Inbox{}, casepkg.Invalid("an inbox cannot escalate to itself")
		}
	}
	item, err := s.lockInboxUpdate(ctx, in.TenantID, in.InboxID, in.EscalationInboxID)
	if err != nil {
		return casepkg.Inbox{}, err
	}
	if in.SLADays != nil {
		item.SLADays = in.SLADays
	}
	if in.ClearSLA {
		item.SLADays = nil
	}
	if in.Name != nil {
		item.Name = strings.TrimSpace(*in.Name)
	}
	if in.Status != nil {
		item.Status = *in.Status
	}
	if in.EscalationInboxID != nil {
		item.EscalationInboxID = in.EscalationInboxID
	}
	if in.ClearEscalationInbox {
		item.EscalationInboxID = nil
	}
	if in.AutoAssignEnabled != nil {
		item.AutoAssignEnabled = *in.AutoAssignEnabled
	}
	if in.CaseReviewManual != nil {
		item.CaseReviewManual = *in.CaseReviewManual
	}
	if in.CaseReviewOnCaseCreated != nil {
		item.CaseReviewOnCaseCreated = *in.CaseReviewOnCaseCreated
	}
	if in.CaseReviewOnEscalate != nil {
		item.CaseReviewOnEscalate = *in.CaseReviewOnEscalate
	}
	item.UpdatedAt = s.clock.Now()
	return s.inboxes.Update(ctx, item)
}

type CreateCaseInput struct {
	TenantID    uuid.UUID
	InboxID     uuid.UUID
	Name        string
	AssigneeID  *string
	Type        casepkg.Type
	DecisionIDs []uuid.UUID
}

func (s CaseService) createCase(ctx context.Context, in CreateCaseInput, actorID *string) (casepkg.Case, error) {
	if err := requireIDs(in.TenantID, in.InboxID); err != nil {
		return casepkg.Case{}, err
	}
	for _, id := range in.DecisionIDs {
		if err := requireIDs(id); err != nil {
			return casepkg.Case{}, err
		}
	}
	if err := validActor(actorID); err != nil {
		return casepkg.Case{}, err
	}
	if err := validActor(in.AssigneeID); err != nil {
		return casepkg.Case{}, err
	}
	now := s.clock.Now()
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return casepkg.Case{}, casepkg.Invalid("name is required")
	}
	caseType := in.Type
	if caseType == "" {
		caseType = casepkg.TypeDecision
	}
	if !casepkg.ValidType(caseType) {
		return casepkg.Case{}, casepkg.Invalid("invalid case type")
	}
	if caseType != casepkg.TypeDecision && len(in.DecisionIDs) > 0 {
		return casepkg.Case{}, casepkg.Invalid("decision links require a decision case")
	}
	if _, err := s.activeInbox(ctx, in.TenantID, in.InboxID); err != nil {
		return casepkg.Case{}, err
	}
	if err := s.inboxAccess(ctx, in.TenantID, in.InboxID); err != nil {
		return casepkg.Case{}, err
	}
	if err := s.loadDecisions(ctx, in.TenantID, in.DecisionIDs); err != nil {
		return casepkg.Case{}, err
	}
	if actorID != nil && in.AssigneeID == nil {
		eligible, err := s.memberships.Has(ctx, in.TenantID, in.InboxID, *actorID)
		if err != nil {
			return casepkg.Case{}, err
		}
		if eligible {
			in.AssigneeID = actorID
		}
	}
	if err := s.eligibleAssignee(ctx, in.TenantID, in.InboxID, in.AssigneeID); err != nil {
		return casepkg.Case{}, err
	}
	initialStatus := casepkg.StatusPending
	if actorID != nil {
		initialStatus = casepkg.StatusInvestigating
	}
	item, err := s.cases.Create(ctx, casepkg.Case{
		ID:         s.ids.New(),
		TenantID:   in.TenantID,
		InboxID:    in.InboxID,
		Name:       name,
		Status:     initialStatus,
		Outcome:    casepkg.OutcomeUnset,
		Type:       caseType,
		AssignedTo: in.AssigneeID,
		CreatedAt:  now,
		UpdatedAt:  now,
	})
	if err != nil {
		return casepkg.Case{}, err
	}
	if actorID != nil {
		if err := s.contributors.Add(ctx, in.TenantID, item.ID, *actorID, now); err != nil {
			return casepkg.Case{}, err
		}
	}
	if in.AssigneeID != nil {
		if _, err := s.events.Create(ctx, newEvent(s.ids.New(), in.TenantID, item.ID, actorID, "case_assigned", "", "", "", *in.AssigneeID, "", now)); err != nil {
			return casepkg.Case{}, err
		}
	}
	for _, decisionID := range in.DecisionIDs {
		if _, err := s.AddDecision(ctx, AddDecisionInput{TenantID: in.TenantID, CaseID: item.ID, DecisionID: decisionID}, actorID); err != nil {
			return casepkg.Case{}, err
		}
	}
	if _, err := s.events.Create(ctx, newEvent(s.ids.New(), in.TenantID, item.ID, actorID, "case_created", "", "", "", item.Name, "", now)); err != nil {
		return casepkg.Case{}, err
	}
	return s.GetCase(ctx, in.TenantID, item.ID)
}

func (s CaseService) GetCase(ctx context.Context, tenantID, caseID uuid.UUID) (casepkg.Case, error) {
	return transact(ctx, s, func(tx CaseService) (casepkg.Case, error) { return tx.getCase(ctx, tenantID, caseID) })
}
func (s CaseService) getCase(ctx context.Context, tenantID, caseID uuid.UUID) (casepkg.Case, error) {
	if err := requireIDs(tenantID, caseID); err != nil {
		return casepkg.Case{}, err
	}
	if _, err := access.Tenant(ctx, tenantID); err != nil {
		return casepkg.Case{}, err
	}
	item, err := s.cases.LockShared(ctx, tenantID, caseID)
	if err != nil {
		return casepkg.Case{}, err
	}
	if _, err := s.inboxes.LockShared(ctx, tenantID, item.InboxID); err != nil {
		return casepkg.Case{}, err
	}
	if err := s.inboxAccess(ctx, tenantID, item.InboxID); err != nil {
		return casepkg.Case{}, err
	}
	if item.Contributors, err = s.contributors.List(ctx, tenantID, caseID); err != nil {
		return casepkg.Case{}, fmt.Errorf("load contributors: %w", err)
	}
	if item.Decisions, err = s.decisions.ListByCase(ctx, tenantID, caseID); err != nil {
		return casepkg.Case{}, fmt.Errorf("load decisions: %w", err)
	}
	if item.Screenings, err = s.screenings.ListByCase(ctx, tenantID, caseID); err != nil {
		return casepkg.Case{}, fmt.Errorf("load screenings: %w", err)
	}
	if item.Tags, err = s.tags.ListByCase(ctx, tenantID, caseID); err != nil {
		return casepkg.Case{}, fmt.Errorf("load tags: %w", err)
	}
	if item.Files, err = s.files.ListByCase(ctx, tenantID, caseID); err != nil {
		return casepkg.Case{}, fmt.Errorf("load files: %w", err)
	}
	if item.Events, err = s.events.ListByCase(ctx, tenantID, caseID, 100); err != nil {
		return casepkg.Case{}, fmt.Errorf("load events: %w", err)
	}
	return item, nil
}

func (s CaseService) ListCases(ctx context.Context, tenantID uuid.UUID, filters casepkg.CaseFilters, limit int) ([]casepkg.Case, error) {
	if err := requireIDs(tenantID); err != nil {
		return nil, err
	}
	if err := validLimit(limit); err != nil {
		return nil, err
	}
	for _, status := range filters.Statuses {
		if !casepkg.ValidStatus(status) {
			return nil, casepkg.Invalid("invalid status filter")
		}
	}
	for _, inboxID := range filters.InboxIDs {
		if err := requireIDs(inboxID); err != nil {
			return nil, err
		}
	}
	p, err := access.Tenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	filters.AccessUserID = ""
	if p.Kind == access.User && !p.Admin {
		filters.AccessUserID = p.Subject
	}
	items, err := s.cases.List(ctx, tenantID, filters, limit)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(items))
	for i := range items {
		ids[i] = items[i].ID
	}
	tags, err := s.tags.ListByCases(ctx, tenantID, ids)
	if err != nil {
		return nil, fmt.Errorf("load case tags: %w", err)
	}
	for i := range items {
		items[i].Tags = tags[items[i].ID]
	}
	return items, nil
}

type UpdateCaseInput struct {
	TenantID         uuid.UUID
	CaseID           uuid.UUID
	InboxID          *uuid.UUID
	Name             *string
	Status           *casepkg.Status
	Outcome          *casepkg.Outcome
	BoostReason      *string
	ReviewLevel      *string
	ClearReviewLevel bool
	ClearBoostReason bool
}

func (s CaseService) updateCase(ctx context.Context, in UpdateCaseInput, actorID *string) (casepkg.Case, error) {
	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		return casepkg.Case{}, casepkg.Invalid("name is required")
	}
	if in.Outcome != nil && !casepkg.ValidOutcome(*in.Outcome) {
		return casepkg.Case{}, casepkg.Invalid("invalid outcome")
	}
	if in.Status != nil && !casepkg.ValidStatus(*in.Status) {
		return casepkg.Case{}, casepkg.Invalid("invalid status")
	}
	if in.ReviewLevel != nil && *in.ReviewLevel != "" && !casepkg.ValidReviewLevel(*in.ReviewLevel) {
		return casepkg.Case{}, casepkg.Invalid("invalid review level")
	}
	if in.ClearReviewLevel && in.ReviewLevel != nil {
		return casepkg.Case{}, casepkg.Invalid("conflicting review level settings")
	}
	if in.ClearBoostReason && in.BoostReason != nil {
		return casepkg.Case{}, casepkg.Invalid("conflicting boost settings")
	}
	if in.BoostReason != nil {
		switch *in.BoostReason {
		case "", "unsnoozed", "reassigned", "escalated", "new_decision":
		default:
			return casepkg.Case{}, casepkg.Invalid("invalid boost reason")
		}
	}
	var destinations []uuid.UUID
	if in.InboxID != nil {
		destinations = append(destinations, *in.InboxID)
	}
	item, err := s.caseForMutation(ctx, in.TenantID, in.CaseID, actorID, destinations...)
	if err != nil {
		return casepkg.Case{}, err
	}
	if in.Status != nil {
		if err := casepkg.ValidateStatusTransition(item.Status, *in.Status); err != nil {
			return casepkg.Case{}, err
		}
	}
	if in.InboxID != nil {
		if _, err := s.activeInbox(ctx, in.TenantID, *in.InboxID); err != nil {
			return casepkg.Case{}, err
		}
		if err := s.inboxAccess(ctx, in.TenantID, *in.InboxID); err != nil {
			return casepkg.Case{}, err
		}
		if err := s.eligibleAssignee(ctx, in.TenantID, *in.InboxID, item.AssignedTo); err != nil {
			return casepkg.Case{}, err
		}
	}
	// Membership removal permits historical assignments on closed cases. Before
	// reopening, require that assignment to be valid again in the destination.
	if item.Status == casepkg.StatusClosed && in.Status != nil && *in.Status != casepkg.StatusClosed && in.InboxID == nil {
		if _, err := s.activeInbox(ctx, in.TenantID, item.InboxID); err != nil {
			return casepkg.Case{}, err
		}
		if err := s.eligibleAssignee(ctx, in.TenantID, item.InboxID, item.AssignedTo); err != nil {
			return casepkg.Case{}, err
		}
	}
	now := s.clock.Now()
	var events []casepkg.Event
	addEvent := func(kind, next, prev string) {
		if next != prev {
			events = append(events, newEvent(s.ids.New(), in.TenantID, item.ID, actorID, kind, "", "", "", next, prev, now))
		}
	}
	if in.InboxID != nil {
		prev := item.InboxID.String()
		item.InboxID = *in.InboxID
		addEvent("inbox_changed", item.InboxID.String(), prev)
	}
	if in.Name != nil {
		prev := item.Name
		item.Name = strings.TrimSpace(*in.Name)
		addEvent("name_updated", item.Name, prev)
	}
	if in.Status != nil {
		prev := string(item.Status)
		item.Status = *in.Status
		addEvent("status_updated", string(item.Status), prev)
		if item.Status == casepkg.StatusClosed && item.SnoozedUntil != nil {
			addEvent("case_unsnoozed", "", item.SnoozedUntil.UTC().Format(time.RFC3339Nano))
			item.SnoozedUntil = nil
		}
	}
	if in.Outcome != nil {
		prev := string(item.Outcome)
		item.Outcome = *in.Outcome
		addEvent("outcome_updated", string(item.Outcome), prev)
	}
	previousBoost, previousReview := stringValue(item.BoostReason), stringValue(item.ReviewLevel)
	if in.BoostReason != nil {
		item.BoostReason = in.BoostReason
		if *in.BoostReason == "" {
			item.BoostReason = nil
		}
	}
	if in.ClearBoostReason {
		item.BoostReason = nil
	}
	if in.ReviewLevel != nil {
		item.ReviewLevel = in.ReviewLevel
		if *in.ReviewLevel == "" {
			item.ReviewLevel = nil
		}
	}
	if in.ClearReviewLevel {
		item.ReviewLevel = nil
	}
	addEvent("boost_updated", stringValue(item.BoostReason), previousBoost)
	addEvent("review_level_updated", stringValue(item.ReviewLevel), previousReview)
	item.UpdatedAt = now
	if _, err := s.cases.Update(ctx, item); err != nil {
		return casepkg.Case{}, err
	}
	for _, event := range events {
		if _, err := s.events.Create(ctx, event); err != nil {
			return casepkg.Case{}, err
		}
	}
	if err := s.investigate(ctx, &item, in.Status == nil || *in.Status == casepkg.StatusPending); err != nil {
		return casepkg.Case{}, err
	}
	return s.GetCase(ctx, in.TenantID, in.CaseID)
}

type AddDecisionInput struct {
	TenantID   uuid.UUID
	CaseID     uuid.UUID
	DecisionID uuid.UUID
	ScenarioID *uuid.UUID
	ObjectType string
	ObjectID   string
	PivotValue *string
}

func (s CaseService) addDecision(ctx context.Context, in AddDecisionInput, actorID *string) (casepkg.DecisionLink, error) {
	if err := requireIDs(in.DecisionID); err != nil {
		return casepkg.DecisionLink{}, err
	}
	if in.ScenarioID != nil {
		if err := requireIDs(*in.ScenarioID); err != nil {
			return casepkg.DecisionLink{}, err
		}
	}
	item, err := s.caseForMutation(ctx, in.TenantID, in.CaseID, actorID)
	if err != nil {
		return casepkg.DecisionLink{}, err
	}
	if item.Type != casepkg.TypeDecision {
		return casepkg.DecisionLink{}, casepkg.Invalid("decision links require a decision case")
	}
	if item.Status == casepkg.StatusClosed {
		return casepkg.DecisionLink{}, casepkg.Invalid("cannot add a decision to a closed case")
	}
	if err := s.loadDecisions(ctx, in.TenantID, []uuid.UUID{in.DecisionID}); err != nil {
		return casepkg.DecisionLink{}, err
	}
	ref := s.decisionRefs[in.DecisionID]
	if err := validateDecisionMetadata(ref, in.ScenarioID, in.ObjectType, in.ObjectID); err != nil {
		return casepkg.DecisionLink{}, err
	}
	in.ScenarioID = &ref.ScenarioID
	in.ObjectType = ref.ObjectType
	in.ObjectID = ref.ObjectID
	// The owning decision service currently exposes object identity, not a pivot field.
	pivot := ref.ObjectType + ":" + ref.ObjectID
	in.PivotValue = &pivot
	now := s.clock.Now()
	link, err := s.decisions.Create(ctx, casepkg.DecisionLink{
		ID: s.ids.New(), TenantID: in.TenantID, CaseID: in.CaseID, DecisionID: in.DecisionID,
		ScenarioID: in.ScenarioID, ObjectType: in.ObjectType, ObjectID: in.ObjectID, PivotValue: in.PivotValue, CreatedAt: now,
	})
	if err != nil {
		return casepkg.DecisionLink{}, err
	}
	if _, err := s.events.Create(ctx, newEvent(s.ids.New(), in.TenantID, in.CaseID, actorID, "decision_added", "", in.DecisionID.String(), "decision", "", "", now)); err != nil {
		return casepkg.DecisionLink{}, err
	}
	if err := s.investigate(ctx, &item, true); err != nil {
		return casepkg.DecisionLink{}, err
	}
	return link, nil
}

func (s CaseService) assign(ctx context.Context, tenantID, caseID uuid.UUID, assignee *string, actorID *string) error {
	if err := validActor(assignee); err != nil {
		return err
	}
	item, err := s.caseForMutation(ctx, tenantID, caseID, actorID)
	if err != nil {
		return err
	}
	if err := s.eligibleAssignee(ctx, tenantID, item.InboxID, assignee); err != nil {
		return err
	}
	now := s.clock.Now()
	if err := s.cases.Assign(ctx, tenantID, caseID, assignee, now); err != nil {
		return err
	}
	newValue := ""
	if assignee != nil {
		newValue = *assignee
	}
	previous := ""
	if item.AssignedTo != nil {
		previous = *item.AssignedTo
	}
	_, err = s.events.Create(ctx, newEvent(s.ids.New(), tenantID, caseID, actorID, "case_assigned", "", "", "", newValue, previous, now))
	if err != nil {
		return err
	}
	if actorID != nil {
		return s.contributors.Add(ctx, tenantID, caseID, *actorID, now)
	}
	return nil
}

func (s CaseService) snooze(ctx context.Context, tenantID, caseID uuid.UUID, until *time.Time, actorID *string) error {
	now := s.clock.Now()
	if until != nil && !until.After(now) {
		return casepkg.Invalid("snooze time must be in the future")
	}
	item, err := s.caseForMutation(ctx, tenantID, caseID, actorID)
	if err != nil {
		return err
	}
	if item.Status == casepkg.StatusClosed {
		return casepkg.Invalid("cannot snooze or unsnooze a closed case")
	}
	if err := s.cases.Snooze(ctx, tenantID, caseID, until, now); err != nil {
		return err
	}
	eventType := "case_unsnoozed"
	newValue := ""
	if until != nil {
		eventType = "case_snoozed"
		newValue = until.Format(time.RFC3339)
	}
	previous := ""
	if item.SnoozedUntil != nil {
		previous = item.SnoozedUntil.Format(time.RFC3339)
	}
	_, err = s.events.Create(ctx, newEvent(s.ids.New(), tenantID, caseID, actorID, eventType, "", "", "", newValue, previous, now))
	if err != nil {
		return err
	}
	item.SnoozedUntil = until
	return s.investigate(ctx, &item, true)
}

func (s CaseService) createComment(ctx context.Context, tenantID, caseID uuid.UUID, comment string, actorID *string) (casepkg.Event, error) {
	comment = strings.TrimSpace(comment)
	if comment == "" {
		return casepkg.Event{}, casepkg.Invalid("comment is required")
	}
	item, err := s.caseForMutation(ctx, tenantID, caseID, actorID)
	if err != nil {
		return casepkg.Event{}, err
	}
	event, err := s.events.Create(ctx, newEvent(s.ids.New(), tenantID, caseID, actorID, "comment_added", comment, "", "", "", "", s.clock.Now()))
	if err != nil {
		return casepkg.Event{}, err
	}
	if err := s.investigate(ctx, &item, true); err != nil {
		return casepkg.Event{}, err
	}
	return event, nil
}

func (s CaseService) ListEvents(ctx context.Context, tenantID, caseID uuid.UUID, limit int) ([]casepkg.Event, error) {
	return transact(ctx, s, func(tx CaseService) ([]casepkg.Event, error) { return tx.listEvents(ctx, tenantID, caseID, limit) })
}
func (s CaseService) listEvents(ctx context.Context, tenantID, caseID uuid.UUID, limit int) ([]casepkg.Event, error) {
	if err := requireIDs(tenantID, caseID); err != nil {
		return nil, err
	}
	if err := validLimit(limit); err != nil {
		return nil, err
	}
	if _, err := access.Tenant(ctx, tenantID); err != nil {
		return nil, err
	}
	item, err := s.cases.LockShared(ctx, tenantID, caseID)
	if err != nil {
		return nil, err
	}
	if _, err := s.inboxes.LockShared(ctx, tenantID, item.InboxID); err != nil {
		return nil, err
	}
	if err := s.inboxAccess(ctx, tenantID, item.InboxID); err != nil {
		return nil, err
	}
	return s.events.ListByCase(ctx, tenantID, caseID, limit)
}

type CreateTagInput struct {
	TenantID uuid.UUID
	Name     string
	Color    string
	Target   string
}

func (s CaseService) createTag(ctx context.Context, in CreateTagInput) (casepkg.Tag, error) {
	if err := requireIDs(in.TenantID); err != nil {
		return casepkg.Tag{}, err
	}
	if strings.TrimSpace(in.Name) == "" {
		return casepkg.Tag{}, casepkg.Invalid("tag name is required")
	}
	now := s.clock.Now()
	target := in.Target
	if target == "" {
		target = "case"
	}
	if target != "case" {
		return casepkg.Tag{}, casepkg.Invalid("unsupported tag target")
	}
	return s.tags.Create(ctx, casepkg.Tag{
		ID: s.ids.New(), TenantID: in.TenantID, Target: target, Name: strings.TrimSpace(in.Name), Color: strings.TrimSpace(in.Color), CreatedAt: now, UpdatedAt: now,
	})
}

func (s CaseService) ListTags(ctx context.Context, tenantID uuid.UUID, target string) ([]casepkg.Tag, error) {
	if _, err := access.Tenant(ctx, tenantID); err != nil {
		return nil, err
	}
	if err := requireIDs(tenantID); err != nil {
		return nil, err
	}
	if target != "" && target != "case" {
		return nil, casepkg.Invalid("unsupported tag target")
	}
	return s.tags.List(ctx, tenantID, target)
}

func (s CaseService) addTag(ctx context.Context, tenantID, caseID, tagID uuid.UUID, actorID *string) error {
	item, err := s.caseForMutation(ctx, tenantID, caseID, actorID)
	if err != nil {
		return err
	}
	if err := s.caseTag(ctx, tenantID, tagID); err != nil {
		return err
	}
	now := s.clock.Now()
	if err := s.tags.AddToCase(ctx, tenantID, caseID, tagID, s.ids.New(), now); err != nil {
		return err
	}
	_, err = s.events.Create(ctx, newEvent(s.ids.New(), tenantID, caseID, actorID, "tags_updated", "", tagID.String(), "case_tag", tagID.String(), "", now))
	if err != nil {
		return err
	}
	return s.investigate(ctx, &item, true)
}

func (s CaseService) removeTag(ctx context.Context, tenantID, caseID, tagID uuid.UUID, actorID *string) error {
	item, err := s.caseForMutation(ctx, tenantID, caseID, actorID)
	if err != nil {
		return err
	}
	if _, err := s.tags.Get(ctx, tenantID, tagID); err != nil {
		return err
	}
	now := s.clock.Now()
	if err := s.tags.RemoveFromCase(ctx, tenantID, caseID, tagID, now); err != nil {
		return err
	}
	_, err = s.events.Create(ctx, newEvent(s.ids.New(), tenantID, caseID, actorID, "tags_updated", "", tagID.String(), "case_tag", "", tagID.String(), now))
	if err != nil {
		return err
	}
	return s.investigate(ctx, &item, true)
}

func (s CaseService) addFile(ctx context.Context, file casepkg.File, actorID *string) (casepkg.File, error) {
	if file.SourceFileID == nil || *file.SourceFileID == uuid.Nil {
		return casepkg.File{}, casepkg.Invalid("source_file_id is required; raw storage keys are not accepted")
	}
	item, err := s.caseForMutation(ctx, file.TenantID, file.CaseID, actorID)
	if err != nil {
		return casepkg.File{}, err
	}
	file, err = s.references.ScreeningFile(ctx, file.TenantID, file.CaseID, *file.SourceFileID)
	if err != nil {
		return casepkg.File{}, err
	}
	if strings.TrimSpace(file.FileName) == "" || strings.TrimSpace(file.StorageKey) == "" || file.FileSize < 0 {
		return casepkg.File{}, casepkg.Invalid("source file metadata is incomplete")
	}
	now := s.clock.Now()
	file.ID = s.ids.New()
	file.CreatedAt = now
	created, err := s.files.Create(ctx, file)
	if err != nil {
		return casepkg.File{}, err
	}
	if _, err := s.events.Create(ctx, newEvent(s.ids.New(), file.TenantID, file.CaseID, actorID, "file_added", "", created.ID.String(), "case_file", created.FileName, "", now)); err != nil {
		return casepkg.File{}, err
	}
	if err := s.investigate(ctx, &item, true); err != nil {
		return casepkg.File{}, err
	}
	return created, nil
}

type WorkflowActionInput struct {
	WorkflowExecutionID uuid.UUID
	TenantID            uuid.UUID
	DecisionID          uuid.UUID
	ScenarioID          *uuid.UUID
	ActionType          string
	ActionConfig        WorkflowActionConfig
}

type WorkflowActionConfig struct {
	URL           string          `json:"url"`
	TitleTemplate json.RawMessage `json:"title_template"`
	InboxID       uuid.UUID       `json:"inbox_id"`
	Name          string          `json:"name"`
	Title         string          `json:"title"`
	TagIDs        []uuid.UUID     `json:"tag_ids"`
	PivotValue    *string         `json:"pivot_value"`
	ObjectType    string          `json:"object_type"`
	ObjectID      string          `json:"object_id"`
	AnyInbox      bool            `json:"any_inbox"`
}

func (s CaseService) handleWorkflowAction(ctx context.Context, in WorkflowActionInput) (casepkg.Case, error) {
	if in.ActionConfig.URL != "" || len(in.ActionConfig.TitleTemplate) > 0 {
		return casepkg.Case{}, casepkg.Invalid("case actions accept literal name/title and no dispatcher URL or title_template")
	}
	if err := requireIDs(in.TenantID, in.WorkflowExecutionID, in.DecisionID); err != nil {
		return casepkg.Case{}, err
	}
	switch in.ActionType {
	case "create_case", "add_to_case", "add_to_case_if_possible":
	default:
		return casepkg.Case{}, casepkg.Invalid("unsupported case workflow action")
	}
	if in.ActionConfig.InboxID == uuid.Nil {
		return casepkg.Case{}, casepkg.Invalid("action_config.inbox_id is required")
	}
	if in.ScenarioID != nil {
		if err := requireIDs(*in.ScenarioID); err != nil {
			return casepkg.Case{}, err
		}
	}
	if _, err := s.activeInbox(ctx, in.TenantID, in.ActionConfig.InboxID); err != nil {
		return casepkg.Case{}, err
	}
	for _, tagID := range in.ActionConfig.TagIDs {
		if err := s.caseTag(ctx, in.TenantID, tagID); err != nil {
			return casepkg.Case{}, err
		}
	}
	if err := s.loadDecisions(ctx, in.TenantID, []uuid.UUID{in.DecisionID}); err != nil {
		return casepkg.Case{}, err
	}
	ref := s.decisionRefs[in.DecisionID]
	if err := validateDecisionMetadata(ref, in.ScenarioID, in.ActionConfig.ObjectType, in.ActionConfig.ObjectID); err != nil {
		return casepkg.Case{}, err
	}
	pivot := ref.ObjectType + ":" + ref.ObjectID
	in.ActionConfig.PivotValue = &pivot
	// All reuse modes share one tenant/object lock, including any_inbox calls.
	// Different objects remain independent; scoped and cross-inbox calls cannot
	// race to create separate cases for the same object.
	if err := s.intake.Lock(ctx, in.TenantID, "pivot", pivot); err != nil {
		return casepkg.Case{}, err
	}
	name := firstNonEmpty(in.ActionConfig.Name, in.ActionConfig.Title, "Workflow case")
	var item casepkg.Case
	if in.ActionType == "add_to_case" || in.ActionType == "add_to_case_if_possible" {
		var inboxID *uuid.UUID
		if !in.ActionConfig.AnyInbox {
			inboxID = &in.ActionConfig.InboxID
		}
		if in.ActionConfig.PivotValue != nil {
			existing, err := s.decisions.FindOpenByPivot(ctx, in.TenantID, inboxID, *in.ActionConfig.PivotValue)
			if err != nil {
				return casepkg.Case{}, err
			}
			if existing != nil {
				item = *existing
			}
		}
	}
	if item.ID == uuid.Nil {
		if in.ActionType == "add_to_case" {
			return casepkg.Case{}, casepkg.ErrNotFound
		}
		created, err := s.CreateCase(ctx, CreateCaseInput{TenantID: in.TenantID, InboxID: in.ActionConfig.InboxID, Name: name, Type: casepkg.TypeDecision}, nil)
		if err != nil {
			return casepkg.Case{}, err
		}
		item = created
	}
	if in.DecisionID != uuid.Nil {
		_, err := s.AddDecision(ctx, AddDecisionInput{
			TenantID: in.TenantID, CaseID: item.ID, DecisionID: in.DecisionID, ScenarioID: in.ScenarioID,
			ObjectType: in.ActionConfig.ObjectType, ObjectID: in.ActionConfig.ObjectID, PivotValue: in.ActionConfig.PivotValue,
		}, nil)
		if err != nil {
			return casepkg.Case{}, err
		}
	}
	for _, tagID := range in.ActionConfig.TagIDs {
		if err := s.AddTag(ctx, in.TenantID, item.ID, tagID, nil); err != nil {
			return casepkg.Case{}, err
		}
	}
	return s.GetCase(ctx, in.TenantID, item.ID)
}

func (s CaseService) handleScreeningReviewed(ctx context.Context, tenantID, screeningID uuid.UUID, decisionID *uuid.UUID, matchID string, status string, reviewerID *string) error {
	if err := requireIDs(tenantID, screeningID); err != nil {
		return err
	}
	if strings.TrimSpace(matchID) == "" {
		return casepkg.Invalid("match ID is required")
	}
	switch status {
	case "pending", "confirmed_hit", "no_hit", "skipped":
	default:
		return casepkg.Invalid("invalid screening match status")
	}
	ref, err := s.references.Screening(ctx, tenantID, screeningID, matchID)
	if err != nil {
		return err
	}
	if decisionID != nil && (ref.DecisionID == nil || *decisionID != *ref.DecisionID) {
		return casepkg.Invalid("screening decision does not match")
	}
	item, err := s.screenings.FindCase(ctx, tenantID, screeningID)
	if err != nil {
		return err
	}
	if item == nil && ref.DecisionID != nil {
		item, err = s.decisions.FindCase(ctx, tenantID, *ref.DecisionID)
		if err != nil {
			return err
		}
	}
	if item == nil {
		if ref.ReviewInboxID == nil {
			return casepkg.Invalid("no unambiguous configured review inbox")
		}
		c, err := s.CreateCase(ctx, CreateCaseInput{TenantID: tenantID, InboxID: *ref.ReviewInboxID, Name: "Screening review", Type: casepkg.TypeContinuousScreening}, nil)
		if err != nil {
			return err
		}
		item = &c
	}
	if _, err := s.caseForMutation(ctx, tenantID, item.ID, nil); err != nil {
		return err
	}
	now := s.clock.Now()
	// Delivery may be reordered after retries. Preserve the event in history,
	// but display the match's current authoritative status rather than regressing.
	if _, err := s.screenings.Create(ctx, casepkg.ScreeningLink{ID: s.ids.New(), TenantID: tenantID, CaseID: item.ID, ScreeningID: screeningID, MatchID: &matchID, Status: ref.MatchStatus, CreatedAt: now}); err != nil {
		return err
	}
	_, err = s.events.Create(ctx, newEvent(s.ids.New(), tenantID, item.ID, nil, "screening_match_reviewed", "", matchID, "continuous_screening_match", status, "", now))
	return err
}
func newEvent(id, tenantID, caseID uuid.UUID, userID *string, eventType, note, resourceID, resourceType, newValue, previousValue string, createdAt time.Time) casepkg.Event {
	return casepkg.Event{ID: id, TenantID: tenantID, CaseID: caseID, UserID: userID, EventType: eventType, AdditionalNote: note, ResourceID: resourceID, ResourceType: resourceType, NewValue: newValue, PreviousValue: previousValue, CreatedAt: createdAt}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
