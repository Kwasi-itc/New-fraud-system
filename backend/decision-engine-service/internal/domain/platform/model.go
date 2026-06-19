package platform

import "time"

type CustomList struct {
	ID          string
	TenantID    string
	Name        string
	Description string
	Kind        string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type CustomListEntry struct {
	ID        string
	TenantID  string
	ListID    *string
	ListName  string
	Value     string
	CreatedAt time.Time
}

type RecordTag struct {
	ID         string
	TenantID   string
	ObjectType string
	ObjectID   string
	Tag        string
	CreatedAt  time.Time
}

type RiskSnapshot struct {
	ID         string
	TenantID   string
	ObjectType string
	ObjectID   string
	RiskLevel  string
	CreatedAt  time.Time
}

type IPFlag struct {
	ID        string
	TenantID  string
	IPAddress string
	Flag      string
	CreatedAt time.Time
}
