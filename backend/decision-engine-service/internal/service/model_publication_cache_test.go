package service

import (
	"context"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"golang.org/x/sync/singleflight"
)

func TestDecisionModelCachePreservesPublicationAndSnapshotIsolation(t *testing.T) {
	reader := &countingDataModelReader{model: ports.TenantModel{RevisionID: "model-v1", RecordLookupField: "object_id", Tables: map[string]ports.TenantModelTable{
		"wallets": {ID: "table-id", Name: "wallets", PublishedVersion: "publication-v1", Fields: map[string]ports.TenantModelField{"state": {Name: "state", Type: "string", IsEnum: true, EnumValues: []string{"open", "closed"}}}, LinksToSingle: map[string]ports.TenantModelLink{"owner": {Name: "owner", ParentTableName: "users"}}},
	}}}
	svc := DecisionService{dataModelReader: reader, metadataLoadGroup: &singleflight.Group{}, evaluationCache: newDecisionEvaluationCache(time.Minute)}
	for i := 0; i < 3; i++ {
		snapshot, err := svc.getTenantModel(context.Background(), "tenant-1")
		if err != nil {
			t.Fatal(err)
		}
		table := snapshot.Tables["wallets"]
		if table.ID != "table-id" || table.PublishedVersion != "publication-v1" || table.Fields["state"].EnumValues[0] != "open" || table.LinksToSingle["owner"].ParentTableName != "users" {
			t.Fatalf("publication identity or isolated snapshot lost on read %d: %+v", i, table)
		}
		table.PublishedVersion = "caller-mutation"
		table.Fields["state"].EnumValues[0] = "changed"
		delete(table.LinksToSingle, "owner")
		snapshot.Tables["wallets"] = table
	}
	if reader.count != 1 {
		t.Fatalf("model loaded %d times; expected cache hit", reader.count)
	}
}
