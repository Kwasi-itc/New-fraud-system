package integration

import (
	"encoding/json"
	"time"
)

type OutboxStatus string

const (
	OutboxStatusPending OutboxStatus = "pending"
	OutboxStatusSent    OutboxStatus = "sent"
	OutboxStatusFailed  OutboxStatus = "failed"
)

type OutboxEvent struct {
	ID            string
	TenantID      string
	AggregateType string
	AggregateID   string
	EventType     string
	Payload       json.RawMessage
	Status        OutboxStatus
	CreatedAt     time.Time
}
