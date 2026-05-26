package scenario

import "time"

type PublicationAction string

const (
	PublicationActionPublish   PublicationAction = "publish"
	PublicationActionUnpublish PublicationAction = "unpublish"
)

type Publication struct {
	ID          string
	TenantID    string
	ScenarioID  string
	IterationID string
	Action      PublicationAction
	CreatedAt   time.Time
}

type PublicationPreparationStatus struct {
	ScenarioID          string
	IterationID         string
	PreparationRequired bool
	PreparationStarted  bool
	PreparationFinished bool
	PendingItems        int
}
