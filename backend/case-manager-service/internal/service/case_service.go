package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	casepkg "github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/domain/case"
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/ports"
)

type CaseService struct {
	ids        ports.IDGenerator
	clock      ports.Clock
	inboxes    ports.InboxRepository
	cases      ports.CaseRepository
	decisions  ports.DecisionLinkRepository
	screenings ports.ScreeningLinkRepository
	tags       ports.TagRepository
	events     ports.EventRepository
	files      ports.FileRepository
}

func NewCaseService(
	ids ports.IDGenerator,
	clock ports.Clock,
	inboxes ports.InboxRepository,
	cases ports.CaseRepository,
	decisions ports.DecisionLinkRepository,
	screenings ports.ScreeningLinkRepository,
	tags ports.TagRepository,
	events ports.EventRepository,
	files ports.FileRepository,
) CaseService {
	return CaseService{ids: ids, clock: clock, inboxes: inboxes, cases: cases, decisions: decisions, screenings: screenings, tags: tags, events: events, files: files}
}

type CreateInboxInput struct {
	TenantID          uuid.UUID
	Name              string
	EscalationInboxID *uuid.UUID
}

func (s CaseService) CreateInbox(ctx context.Context, in CreateInboxInput) (casepkg.Inbox, error) {
	now := s.clock.Now()
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return casepkg.Inbox{}, fmt.Errorf("name is required")
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
	return s.inboxes.List(ctx, tenantID)
}

func (s CaseService) GetInbox(ctx context.Context, tenantID, inboxID uuid.UUID) (casepkg.Inbox, error) {
	return s.inboxes.Get(ctx, tenantID, inboxID)
}

type UpdateInboxInput struct {
	TenantID                uuid.UUID
	InboxID                 uuid.UUID
	Name                    *string
	Status                  *string
	EscalationInboxID       *uuid.UUID
	AutoAssignEnabled       *bool
	CaseReviewManual        *bool
	CaseReviewOnCaseCreated *bool
	CaseReviewOnEscalate    *bool
}

func (s CaseService) UpdateInbox(ctx context.Context, in UpdateInboxInput) (casepkg.Inbox, error) {
	item, err := s.inboxes.Get(ctx, in.TenantID, in.InboxID)
	if err != nil {
		return casepkg.Inbox{}, err
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

func (s CaseService) CreateCase(ctx context.Context, in CreateCaseInput, actorID *string) (casepkg.Case, error) {
	now := s.clock.Now()
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return casepkg.Case{}, fmt.Errorf("name is required")
	}
	caseType := in.Type
	if caseType == "" {
		caseType = casepkg.TypeDecision
	}
	item, err := s.cases.Create(ctx, casepkg.Case{
		ID:         s.ids.New(),
		TenantID:   in.TenantID,
		InboxID:    in.InboxID,
		Name:       name,
		Status:     casepkg.StatusPending,
		Outcome:    casepkg.OutcomeUnset,
		Type:       caseType,
		AssignedTo: in.AssigneeID,
		CreatedAt:  now,
		UpdatedAt:  now,
	})
	if err != nil {
		return casepkg.Case{}, err
	}
	for _, decisionID := range in.DecisionIDs {
		_, _ = s.AddDecision(ctx, AddDecisionInput{TenantID: in.TenantID, CaseID: item.ID, DecisionID: decisionID}, actorID)
	}
	_, _ = s.events.Create(ctx, newEvent(s.ids.New(), in.TenantID, item.ID, actorID, "case_created", "", "", "", item.Name, "", now))
	return s.GetCase(ctx, in.TenantID, item.ID)
}

func (s CaseService) GetCase(ctx context.Context, tenantID, caseID uuid.UUID) (casepkg.Case, error) {
	item, err := s.cases.Get(ctx, tenantID, caseID)
	if err != nil {
		return casepkg.Case{}, err
	}
	item.Decisions, _ = s.decisions.ListByCase(ctx, tenantID, caseID)
	item.Screenings, _ = s.screenings.ListByCase(ctx, tenantID, caseID)
	item.Tags, _ = s.tags.ListByCase(ctx, tenantID, caseID)
	item.Files, _ = s.files.ListByCase(ctx, tenantID, caseID)
	item.Events, _ = s.events.ListByCase(ctx, tenantID, caseID, 100)
	return item, nil
}

func (s CaseService) ListCases(ctx context.Context, tenantID uuid.UUID, filters casepkg.CaseFilters, limit int) ([]casepkg.Case, error) {
	items, err := s.cases.List(ctx, tenantID, filters, limit)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Tags, _ = s.tags.ListByCase(ctx, tenantID, items[i].ID)
	}
	return items, nil
}

type UpdateCaseInput struct {
	TenantID    uuid.UUID
	CaseID      uuid.UUID
	InboxID     *uuid.UUID
	Name        *string
	Status      *casepkg.Status
	Outcome     *casepkg.Outcome
	BoostReason *string
	ReviewLevel *string
}

func (s CaseService) UpdateCase(ctx context.Context, in UpdateCaseInput, actorID *string) (casepkg.Case, error) {
	item, err := s.cases.Get(ctx, in.TenantID, in.CaseID)
	if err != nil {
		return casepkg.Case{}, err
	}
	now := s.clock.Now()
	if in.InboxID != nil {
		prev := item.InboxID.String()
		item.InboxID = *in.InboxID
		_, _ = s.events.Create(ctx, newEvent(s.ids.New(), in.TenantID, item.ID, actorID, "inbox_changed", "", "", "", item.InboxID.String(), prev, now))
	}
	if in.Name != nil {
		prev := item.Name
		item.Name = strings.TrimSpace(*in.Name)
		_, _ = s.events.Create(ctx, newEvent(s.ids.New(), in.TenantID, item.ID, actorID, "name_updated", "", "", "", item.Name, prev, now))
	}
	if in.Status != nil {
		if err := casepkg.ValidateStatusTransition(item.Status, *in.Status); err != nil {
			return casepkg.Case{}, err
		}
		prev := string(item.Status)
		item.Status = *in.Status
		_, _ = s.events.Create(ctx, newEvent(s.ids.New(), in.TenantID, item.ID, actorID, "status_updated", "", "", "", string(item.Status), prev, now))
	}
	if in.Outcome != nil {
		prev := string(item.Outcome)
		item.Outcome = *in.Outcome
		_, _ = s.events.Create(ctx, newEvent(s.ids.New(), in.TenantID, item.ID, actorID, "outcome_updated", "", "", "", string(item.Outcome), prev, now))
	}
	if in.BoostReason != nil {
		item.BoostReason = in.BoostReason
	}
	if in.ReviewLevel != nil {
		if *in.ReviewLevel != "" && !casepkg.ValidReviewLevel(*in.ReviewLevel) {
			return casepkg.Case{}, fmt.Errorf("invalid review level")
		}
		item.ReviewLevel = in.ReviewLevel
	}
	item.UpdatedAt = now
	if _, err := s.cases.Update(ctx, item); err != nil {
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

func (s CaseService) AddDecision(ctx context.Context, in AddDecisionInput, actorID *string) (casepkg.DecisionLink, error) {
	now := s.clock.Now()
	link, err := s.decisions.Create(ctx, casepkg.DecisionLink{
		ID: s.ids.New(), TenantID: in.TenantID, CaseID: in.CaseID, DecisionID: in.DecisionID,
		ScenarioID: in.ScenarioID, ObjectType: in.ObjectType, ObjectID: in.ObjectID, PivotValue: in.PivotValue, CreatedAt: now,
	})
	if err != nil {
		return casepkg.DecisionLink{}, err
	}
	_, _ = s.events.Create(ctx, newEvent(s.ids.New(), in.TenantID, in.CaseID, actorID, "decision_added", "", in.DecisionID.String(), "decision", "", "", now))
	return link, nil
}

func (s CaseService) Assign(ctx context.Context, tenantID, caseID uuid.UUID, assignee *string, actorID *string) error {
	now := s.clock.Now()
	if err := s.cases.Assign(ctx, tenantID, caseID, assignee, now); err != nil {
		return err
	}
	newValue := ""
	if assignee != nil {
		newValue = *assignee
	}
	_, _ = s.events.Create(ctx, newEvent(s.ids.New(), tenantID, caseID, actorID, "case_assigned", "", "", "", newValue, "", now))
	return nil
}

func (s CaseService) Snooze(ctx context.Context, tenantID, caseID uuid.UUID, until *time.Time, actorID *string) error {
	now := s.clock.Now()
	if err := s.cases.Snooze(ctx, tenantID, caseID, until, now); err != nil {
		return err
	}
	eventType := "case_unsnoozed"
	newValue := ""
	if until != nil {
		eventType = "case_snoozed"
		newValue = until.Format(time.RFC3339)
	}
	_, _ = s.events.Create(ctx, newEvent(s.ids.New(), tenantID, caseID, actorID, eventType, "", "", "", newValue, "", now))
	return nil
}

func (s CaseService) CreateComment(ctx context.Context, tenantID, caseID uuid.UUID, comment string, actorID *string) (casepkg.Event, error) {
	comment = strings.TrimSpace(comment)
	if comment == "" {
		return casepkg.Event{}, fmt.Errorf("comment is required")
	}
	return s.events.Create(ctx, newEvent(s.ids.New(), tenantID, caseID, actorID, "comment_added", comment, "", "", "", "", s.clock.Now()))
}

func (s CaseService) ListEvents(ctx context.Context, tenantID, caseID uuid.UUID, limit int) ([]casepkg.Event, error) {
	return s.events.ListByCase(ctx, tenantID, caseID, limit)
}

type CreateTagInput struct {
	TenantID uuid.UUID
	Name     string
	Color    string
	Target   string
}

func (s CaseService) CreateTag(ctx context.Context, in CreateTagInput) (casepkg.Tag, error) {
	now := s.clock.Now()
	target := in.Target
	if target == "" {
		target = "case"
	}
	return s.tags.Create(ctx, casepkg.Tag{
		ID: s.ids.New(), TenantID: in.TenantID, Target: target, Name: strings.TrimSpace(in.Name), Color: strings.TrimSpace(in.Color), CreatedAt: now, UpdatedAt: now,
	})
}

func (s CaseService) ListTags(ctx context.Context, tenantID uuid.UUID, target string) ([]casepkg.Tag, error) {
	return s.tags.List(ctx, tenantID, target)
}

func (s CaseService) AddTag(ctx context.Context, tenantID, caseID, tagID uuid.UUID, actorID *string) error {
	now := s.clock.Now()
	if err := s.tags.AddToCase(ctx, tenantID, caseID, tagID, s.ids.New(), now); err != nil {
		return err
	}
	_, _ = s.events.Create(ctx, newEvent(s.ids.New(), tenantID, caseID, actorID, "tags_updated", "", tagID.String(), "case_tag", tagID.String(), "", now))
	return nil
}

func (s CaseService) RemoveTag(ctx context.Context, tenantID, caseID, tagID uuid.UUID, actorID *string) error {
	now := s.clock.Now()
	if err := s.tags.RemoveFromCase(ctx, tenantID, caseID, tagID, now); err != nil {
		return err
	}
	_, _ = s.events.Create(ctx, newEvent(s.ids.New(), tenantID, caseID, actorID, "tags_updated", "", tagID.String(), "case_tag", "", tagID.String(), now))
	return nil
}

func (s CaseService) AddFile(ctx context.Context, file casepkg.File, actorID *string) (casepkg.File, error) {
	now := s.clock.Now()
	file.ID = s.ids.New()
	file.CreatedAt = now
	created, err := s.files.Create(ctx, file)
	if err != nil {
		return casepkg.File{}, err
	}
	_, _ = s.events.Create(ctx, newEvent(s.ids.New(), file.TenantID, file.CaseID, actorID, "file_added", "", created.ID.String(), "case_file", created.FileName, "", now))
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
	InboxID    uuid.UUID   `json:"inbox_id"`
	Name       string      `json:"name"`
	Title      string      `json:"title"`
	TagIDs     []uuid.UUID `json:"tag_ids"`
	PivotValue *string     `json:"pivot_value"`
	ObjectType string      `json:"object_type"`
	ObjectID   string      `json:"object_id"`
	AnyInbox   bool        `json:"any_inbox"`
}

func (s CaseService) HandleWorkflowAction(ctx context.Context, in WorkflowActionInput) (casepkg.Case, error) {
	if in.ActionConfig.InboxID == uuid.Nil {
		return casepkg.Case{}, fmt.Errorf("action_config.inbox_id is required")
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
		created, err := s.CreateCase(ctx, CreateCaseInput{TenantID: in.TenantID, InboxID: in.ActionConfig.InboxID, Name: name, Type: casepkg.TypeDecision}, nil)
		if err != nil {
			return casepkg.Case{}, err
		}
		item = created
	}
	if in.DecisionID != uuid.Nil {
		_, _ = s.AddDecision(ctx, AddDecisionInput{
			TenantID: in.TenantID, CaseID: item.ID, DecisionID: in.DecisionID, ScenarioID: in.ScenarioID,
			ObjectType: in.ActionConfig.ObjectType, ObjectID: in.ActionConfig.ObjectID, PivotValue: in.ActionConfig.PivotValue,
		}, nil)
	}
	for _, tagID := range in.ActionConfig.TagIDs {
		_ = s.AddTag(ctx, in.TenantID, item.ID, tagID, nil)
	}
	return s.GetCase(ctx, in.TenantID, item.ID)
}

func (s CaseService) HandleScreeningReviewed(ctx context.Context, tenantID, screeningID uuid.UUID, decisionID *uuid.UUID, matchID string, status string, reviewerID *string) error {
	now := s.clock.Now()
	var caseID uuid.UUID
	if decisionID != nil {
		cases, err := s.cases.List(ctx, tenantID, casepkg.CaseFilters{}, 500)
		if err != nil {
			return err
		}
		for _, c := range cases {
			links, _ := s.decisions.ListByCase(ctx, tenantID, c.ID)
			for _, link := range links {
				if link.DecisionID == *decisionID {
					caseID = c.ID
					break
				}
			}
			if caseID != uuid.Nil {
				break
			}
		}
	}
	if caseID == uuid.Nil {
		inboxes, err := s.inboxes.List(ctx, tenantID)
		if err != nil {
			return err
		}
		if len(inboxes) == 0 {
			return fmt.Errorf("no inbox available for screening review")
		}
		c, err := s.CreateCase(ctx, CreateCaseInput{TenantID: tenantID, InboxID: inboxes[0].ID, Name: "Screening review", Type: casepkg.TypeContinuousScreening}, reviewerID)
		if err != nil {
			return err
		}
		caseID = c.ID
	}
	matchIDPtr := &matchID
	_, err := s.screenings.Create(ctx, casepkg.ScreeningLink{ID: s.ids.New(), TenantID: tenantID, CaseID: caseID, ScreeningID: screeningID, MatchID: matchIDPtr, Status: status, CreatedAt: now})
	if err != nil {
		return err
	}
	_, _ = s.events.Create(ctx, newEvent(s.ids.New(), tenantID, caseID, reviewerID, "screening_match_reviewed", "", matchID, "continuous_screening_match", status, "", now))
	return nil
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
