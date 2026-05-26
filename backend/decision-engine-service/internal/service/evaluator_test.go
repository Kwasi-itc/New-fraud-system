package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/decision"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/platform"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	asteval "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/runtime/ast_eval"
)

type stubTenantDataReader struct {
	records []ports.TenantRecord
}

func (s stubTenantDataReader) GetRecord(ctx context.Context, tenantID, objectType, objectID string) (ports.TenantRecord, error) {
	for _, record := range s.records {
		if record.ObjectType == objectType && record.ObjectID == objectID {
			return record, nil
		}
	}
	return ports.TenantRecord{}, nil
}

func (s stubTenantDataReader) ListRecords(ctx context.Context, tenantID, objectType string, limit int) ([]ports.TenantRecord, error) {
	out := make([]ports.TenantRecord, 0, len(s.records))
	for _, record := range s.records {
		if record.ObjectType == objectType {
			out = append(out, record)
		}
	}
	return out, nil
}

func (s stubTenantDataReader) QueryRecords(ctx context.Context, tenantID, objectType, fieldName, value string, limit int) ([]ports.TenantRecord, error) {
	out := make([]ports.TenantRecord, 0, len(s.records))
	for _, record := range s.records {
		if record.ObjectType != objectType {
			continue
		}
		if recordValue, ok := record.Fields[fieldName]; ok && recordValue != nil && recordValue == value {
			out = append(out, record)
		}
	}
	return out, nil
}

type stubCustomListRepo struct {
	values map[string]map[string]bool
}

func (s stubCustomListRepo) Create(ctx context.Context, item platform.CustomListEntry) (platform.CustomListEntry, error) {
	return item, nil
}

func (s stubCustomListRepo) ListByName(ctx context.Context, tenantID, listName string) ([]platform.CustomListEntry, error) {
	return nil, nil
}

func (s stubCustomListRepo) Contains(ctx context.Context, tenantID, listName, value string) (bool, error) {
	return s.values[listName][value], nil
}

type stubRecordTagRepo struct {
	tags map[string]bool
}

func (s stubRecordTagRepo) Create(ctx context.Context, item platform.RecordTag) (platform.RecordTag, error) {
	return item, nil
}

func (s stubRecordTagRepo) ListByObject(ctx context.Context, tenantID, objectType, objectID string) ([]platform.RecordTag, error) {
	return nil, nil
}

func (s stubRecordTagRepo) HasTag(ctx context.Context, tenantID, objectType, objectID, tag string) (bool, error) {
	return s.tags[tag], nil
}

type stubRiskRepo struct {
	snapshot *platform.RiskSnapshot
}

func (s stubRiskRepo) Create(ctx context.Context, item platform.RiskSnapshot) (platform.RiskSnapshot, error) {
	return item, nil
}

func (s stubRiskRepo) GetByObject(ctx context.Context, tenantID, objectType, objectID string) (*platform.RiskSnapshot, error) {
	return s.snapshot, nil
}

type stubIPFlagRepo struct {
	flags map[string]bool
}

func (s stubIPFlagRepo) Create(ctx context.Context, item platform.IPFlag) (platform.IPFlag, error) {
	return item, nil
}

func (s stubIPFlagRepo) HasFlag(ctx context.Context, tenantID, ipAddress, flag string) (bool, error) {
	return s.flags[ipAddress+"|"+flag], nil
}

func (s stubIPFlagRepo) ListByIP(ctx context.Context, tenantID, ipAddress string) ([]platform.IPFlag, error) {
	return nil, nil
}

type stubDecisionRepo struct {
	items []decision.Decision
}

func (s stubDecisionRepo) Create(ctx context.Context, item decision.Decision) (decision.Decision, error) {
	return item, nil
}

func (s stubDecisionRepo) GetByID(ctx context.Context, tenantID, decisionID string) (decision.Decision, error) {
	return decision.Decision{}, nil
}

func (s stubDecisionRepo) ListByTenant(ctx context.Context, tenantID string) ([]decision.Decision, error) {
	return s.items, nil
}

func (s stubDecisionRepo) ListByScenario(ctx context.Context, tenantID, scenarioID string) ([]decision.Decision, error) {
	return nil, nil
}

func (s stubDecisionRepo) ListByObject(ctx context.Context, tenantID, objectType, objectID string) ([]decision.Decision, error) {
	return s.items, nil
}

func mustFormula(t *testing.T, node domainast.Node) json.RawMessage {
	t.Helper()
	payload, err := json.Marshal(node)
	if err != nil {
		t.Fatalf("marshal formula: %v", err)
	}
	return payload
}

func TestEvaluateFormulaPlatformFunctions(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	runtime := asteval.Runtime{
		TenantID:   "tenant-1",
		ObjectID:   "record-1",
		ObjectType: "transactions",
		Fields: map[string]any{
			"ip":         "1.2.3.4",
			"account_id": "acct-1",
			"owner_id":   "record-1",
		},
		Model: &ports.TenantModel{
			RecordLookupField: "object_id",
			Tables: map[string]ports.TenantModelTable{
				"transactions": {
					Name: "transactions",
					Fields: map[string]ports.TenantModelField{
						"ip":         {Name: "ip", Type: "string"},
						"account_id": {Name: "account_id", Type: "string"},
						"owner_id":   {Name: "owner_id", Type: "string"},
					},
					LinksToSingle: map[string]ports.TenantModelLink{
						"account": {
							Name:            "account",
							ParentTableName: "accounts",
							ParentFieldName: "object_id",
							ChildTableName:  "transactions",
							ChildFieldName:  "account_id",
						},
					},
				},
				"accounts": {
					Name: "accounts",
					Fields: map[string]ports.TenantModelField{
						"object_id":  {Name: "object_id", Type: "string"},
						"status":     {Name: "status", Type: "string"},
						"owner_id":   {Name: "owner_id", Type: "string"},
						"profile_id": {Name: "profile_id", Type: "string"},
					},
					LinksToSingle: map[string]ports.TenantModelLink{
						"profile": {
							Name:            "profile",
							ParentTableName: "profiles",
							ParentFieldName: "object_id",
							ChildTableName:  "accounts",
							ChildFieldName:  "profile_id",
						},
					},
				},
				"profiles": {
					Name: "profiles",
					Fields: map[string]ports.TenantModelField{
						"object_id": {Name: "object_id", Type: "string"},
						"tier":      {Name: "tier", Type: "string"},
					},
				},
			},
		},
		TenantDataReader: stubTenantDataReader{
			records: []ports.TenantRecord{
				{ObjectID: "acct-1", ObjectType: "accounts", Fields: map[string]any{"object_id": "acct-1", "status": "active", "owner_id": "record-1", "profile_id": "profile-1"}},
				{ObjectID: "r1", ObjectType: "accounts", Fields: map[string]any{"object_id": "r1", "owner_id": "record-1"}},
				{ObjectID: "r2", ObjectType: "accounts", Fields: map[string]any{"object_id": "r2", "owner_id": "record-1"}},
				{ObjectID: "r3", ObjectType: "accounts", Fields: map[string]any{"object_id": "r3", "owner_id": "other"}},
				{ObjectID: "r4", ObjectType: "accounts", Fields: map[string]any{"object_id": "r4", "owner_id": nil}},
				{ObjectID: "profile-1", ObjectType: "profiles", Fields: map[string]any{"object_id": "profile-1", "tier": "gold"}},
			},
		},
		CustomListRepo: stubCustomListRepo{
			values: map[string]map[string]bool{
				"blocked_emails": {"test@example.com": true},
			},
		},
		RecordTagRepo: stubRecordTagRepo{
			tags: map[string]bool{"high_value": true},
		},
		RiskRepo: stubRiskRepo{
			snapshot: &platform.RiskSnapshot{RiskLevel: "high", CreatedAt: now},
		},
		IPFlagRepo: stubIPFlagRepo{
			flags: map[string]bool{"1.2.3.4|tor": true},
		},
		DecisionRepo: stubDecisionRepo{
			items: []decision.Decision{
				{Outcome: decision.OutcomeReview},
				{Outcome: decision.OutcomeReview},
				{Outcome: decision.OutcomeApprove},
			},
		},
	}

	tests := []struct {
		name    string
		formula domainast.Node
		want    any
	}{
		{
			name: "custom list lookup",
			formula: domainast.Node{
				Function: "in_custom_list",
				NamedChildren: map[string]domainast.Node{
					"list":  {Constant: "blocked_emails"},
					"value": {Constant: "test@example.com"},
				},
			},
			want: true,
		},
		{
			name: "record tag lookup",
			formula: domainast.Node{
				Function: "record_has_tag",
				NamedChildren: map[string]domainast.Node{
					"tag": {Constant: "high_value"},
				},
			},
			want: true,
		},
		{
			name: "risk level lookup",
			formula: domainast.Node{
				Function: "eq",
				Children: []domainast.Node{
					{Function: "record_risk_level"},
					{Constant: "high"},
				},
			},
			want: true,
		},
		{
			name: "ip flag lookup",
			formula: domainast.Node{
				Function: "has_ip_flag",
				NamedChildren: map[string]domainast.Node{
					"ip":   {Function: "field_ref", NamedChildren: map[string]domainast.Node{"field": {Constant: "ip"}}},
					"flag": {Constant: "tor"},
				},
			},
			want: true,
		},
		{
			name: "past decision count with outcome filter",
			formula: domainast.Node{
				Function: "eq",
				Children: []domainast.Node{
					{
						Function: "past_decision_count",
						NamedChildren: map[string]domainast.Node{
							"outcome": {Constant: "review"},
						},
					},
					{Constant: float64(2)},
				},
			},
			want: true,
		},
		{
			name: "past decision count without outcome filter",
			formula: domainast.Node{
				Function: "eq",
				Children: []domainast.Node{
					{
						Function: "past_decision_count",
					},
					{Constant: float64(3)},
				},
			},
			want: true,
		},
		{
			name: "related count with equals filter",
			formula: domainast.Node{
				Function: "eq",
				Children: []domainast.Node{
					{
						Function: "related_count",
						NamedChildren: map[string]domainast.Node{
							"object_type": {Constant: "accounts"},
							"field":       {Constant: "owner_id"},
							"equals":      {Constant: "record-1"},
						},
					},
					{Constant: float64(3)},
				},
			},
			want: true,
		},
		{
			name: "related count with dynamic equals filter",
			formula: domainast.Node{
				Function: "eq",
				Children: []domainast.Node{
					{
						Function: "related_count",
						NamedChildren: map[string]domainast.Node{
							"object_type": {Constant: "accounts"},
							"field":       {Constant: "owner_id"},
							"equals": {
								Function:      "field_ref",
								NamedChildren: map[string]domainast.Node{"field": {Constant: "owner_id"}},
							},
						},
					},
					{Constant: float64(3)},
				},
			},
			want: true,
		},
		{
			name: "related count without equals counts non nil field values",
			formula: domainast.Node{
				Function: "eq",
				Children: []domainast.Node{
					{
						Function: "related_count",
						NamedChildren: map[string]domainast.Node{
							"object_type": {Constant: "accounts"},
							"field":       {Constant: "owner_id"},
						},
					},
					{Constant: float64(4)},
				},
			},
			want: true,
		},
		{
			name: "past decision count with unmatched outcome",
			formula: domainast.Node{
				Function: "eq",
				Children: []domainast.Node{
					{
						Function: "past_decision_count",
						NamedChildren: map[string]domainast.Node{
							"outcome": {Constant: "decline"},
						},
					},
					{Constant: float64(0)},
				},
			},
			want: true,
		},
		{
			name: "related field traversal",
			formula: domainast.Node{
				Function: "eq",
				Children: []domainast.Node{
					{
						Function: "related_field",
						NamedChildren: map[string]domainast.Node{
							"path":  {Constant: "account"},
							"field": {Constant: "status"},
						},
					},
					{Constant: "active"},
				},
			},
			want: true,
		},
		{
			name: "related field nested traversal",
			formula: domainast.Node{
				Function: "eq",
				Children: []domainast.Node{
					{
						Function: "related_field",
						NamedChildren: map[string]domainast.Node{
							"path":  {Constant: "account.profile"},
							"field": {Constant: "tier"},
						},
					},
					{Constant: "gold"},
				},
			},
			want: true,
		},
		{
			name: "related field returns nil when lookup value is missing",
			formula: domainast.Node{
				Function: "is_null",
				Children: []domainast.Node{
					{
						Function: "related_field",
						NamedChildren: map[string]domainast.Node{
							"path":  {Constant: "account"},
							"field": {Constant: "status"},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "related field returns nil when related record is missing",
			formula: domainast.Node{
				Function: "is_null",
				Children: []domainast.Node{
					{
						Function: "related_field",
						NamedChildren: map[string]domainast.Node{
							"path":  {Constant: "account.profile"},
							"field": {Constant: "tier"},
						},
					},
				},
			},
			want: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			testRuntime := runtime
			if tc.name == "related field returns nil when lookup value is missing" {
				testRuntime.Fields = map[string]any{
					"ip":       "1.2.3.4",
					"owner_id": "record-1",
				}
			}
			if tc.name == "related field returns nil when related record is missing" {
				testRuntime.Fields = map[string]any{
					"ip":         "1.2.3.4",
					"account_id": "acct-missing",
					"owner_id":   "record-1",
				}
			}
			got, err := asteval.EvaluateNode(context.Background(), tc.formula, testRuntime)
			if err != nil {
				t.Fatalf("evaluateNode() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("evaluateNode() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEvaluateFormulaBooleanResult(t *testing.T) {
	t.Parallel()

	formula := mustFormula(t, domainast.Node{
		Function: "record_has_tag",
		NamedChildren: map[string]domainast.Node{
			"tag": {Constant: "watchlist"},
		},
	})

	got, err := asteval.EvaluateFormula(context.Background(), formula, asteval.Runtime{
		TenantID:      "tenant-1",
		ObjectID:      "record-1",
		ObjectType:    "transactions",
		Fields:        map[string]any{},
		RecordTagRepo: stubRecordTagRepo{tags: map[string]bool{"watchlist": true}},
	})
	if err != nil {
		t.Fatalf("evaluateFormula() error = %v", err)
	}
	if !got {
		t.Fatalf("evaluateFormula() = false, want true")
	}
}
