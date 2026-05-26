package platform

import "time"

type CustomListEntry struct {
	ID        string
	TenantID  string
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
