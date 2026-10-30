package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

func TestProcessReadBudgetsAndMembership(t *testing.T) {
	_, f, cfg := processFixture(t)
	api := startProcess(t, cfg, false)
	expectHTTP(t, api, "POST", recordPath(f), "seed", `{"object_id":"a","number":1,"ip":"2001:db8::1"}`, 200)
	path := "/v1/tenants/" + f.model.TenantID.String() + "/query/aggregate"
	for _, query := range []string{
		`{"object_type":"records","aggregate":"count","field":"ip"}`,
		`{"object_type":"records","aggregate":"count_distinct","field":"ip"}`,
		`{"object_type":"records","aggregate":"count","field":"number","filter":{"kind":"predicate","field":"object_id","op":"in","value":["a",null]}}`,
		`{"object_type":"records","aggregate":"count","field":"number","filter":{"kind":"predicate","field":"ip","op":"in","value":["2001:0db8::1",null]}}`,
	} {
		data := expectHTTP(t, api, "POST", path, "", query, 200)
		if string(data) != `{"value":1}` {
			t.Fatal(string(data))
		}
	}
	for _, body := range []string{
		`{"object_type":"records","aggregate":"count","field":"number"} {"ignored":true}`,
		strings.Repeat("[", 65) + "0" + strings.Repeat("]", 65),
		`{"object_type":"records","aggregate":"count","field":"number","filter":{"kind":"predicate","field":"number","op":"eq","value":"malformed"}}`,
		`{"object_type":"records","aggregate":"count","field":"number","filter":{"kind":"predicate","field":"ip","op":"in","value":["bad"]}}`,
	} {
		expectHTTP(t, api, "POST", path, "", body, 400)
	}
	values := strings.TrimSuffix(strings.Repeat("1,", 101), ",")
	expectHTTP(t, api, "POST", path, "", fmt.Sprintf(`{"object_type":"records","aggregate":"count","field":"number","filter":{"kind":"predicate","field":"number","op":"in","value":[%s]}}`, values), 400)
	// Every leaf is individually valid, but IP tuples expand to three bound
	// parameters each. The complete statement must reject 20*40*3 > 2048.
	ipValues := strings.TrimSuffix(strings.Repeat(`"2001:db8::1",`, 40), ",")
	leaf := fmt.Sprintf(`{"kind":"predicate","field":"ip","op":"in","value":[%s]}`, ipValues)
	children := strings.TrimSuffix(strings.Repeat(leaf+",", 20), ",")
	expectHTTP(t, api, "POST", path, "", `{"object_type":"records","aggregate":"count","field":"number","filter":{"kind":"group","operator":"or","children":[`+children+`]}}`, 413)
	expectHTTP(t, api, "POST", path, "", strings.Repeat(" ", int(cfg["Limits"].(RecordOwnerLimits).CommandBytes)+(65<<10)), 413)
	// A valid stored key may exceed a read policy. Reject before returning a
	// truncated/partial value and prove subsequent ordinary reads still work.
	key := strings.Repeat("z", 16385)
	body, _ := json.Marshal(map[string]any{"object_id": key, "number": 2})
	expectHTTP(t, api, "POST", recordPath(f), "large", string(body), 200)
	expectHTTP(t, api, "GET", readPath(f)+"?limit=100", "", "", 413)
	expectHTTP(t, api, "POST", path, "", `{"object_type":"records","aggregate":"max","field":"object_id"}`, 413)
	expectHTTP(t, api, "GET", readPath(f)+"/a", "", "", 200)
}

func TestProcessCrossTenantReadAndWork(t *testing.T) {
	shared, f, cfg := processFixture(t)
	api := startProcess(t, cfg, false)
	expectHTTP(t, api, "POST", recordPath(f), "scope", `{"object_id":"a","number":1}`, 200)
	foreignTenant, foreignTable, _ := seedFullContract(t, shared)
	foreignPath := strings.Replace(recordPath(f), f.model.TenantID.String(), foreignTenant.String(), 1)
	expectHTTP(t, api, "POST", foreignPath, "scope", `{"object_id":"a","number":1}`, 403)
	for _, suffix := range []string{"", "/a", "/search?field=number&value=1"} {
		path := strings.Replace(readPath(f), f.model.TenantID.String(), foreignTenant.String(), 1) + suffix
		expectHTTP(t, api, "GET", path, "", "", 403)
	}
	expectHTTP(t, api, "POST", "/v1/tenants/"+foreignTenant.String()+"/query/aggregate", "", `{"object_type":"records","aggregate":"count","field":"number"}`, 403)
	expectHTTP(t, api, "GET", foreignPath+"/upload-logs", "", "", 403)
	postCSV(t, api, foreignPath, "object_id,number\nforeign,2\n", 403)
	ctx := WithServiceCredential(context.Background(), processToken)
	owner, err := NewAuthorizedRecordOwner(f.db, f.database, foreignTenant, foreignTable, "transactions", processToken, cfg["Limits"].(RecordOwnerLimits), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := NewWorkOwner(owner, cfg["UploadQueue"].(string), cfg["DeferredQueue"].(string), 1<<20, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{UploadWork, DeferredWork} {
		input := workInput(kind)
		if _, err = foreign.Create(ctx, input); err != nil {
			t.Fatal(err)
		}
		path := "/v1/deferred-ingests/" + input.ID.String()
		if kind == UploadWork {
			path = "/v1/upload-logs/" + input.ID.String()
		}
		expectHTTP(t, api, "GET", path, "", "", 404)
		operator, err := NewWorkOperator(processWork(t, f, cfg), func(context.Context, *sql.Tx, WorkOperatorScope) (string, error) { return "", ErrOwnerAuthorization })
		if err != nil {
			t.Fatal(err)
		}
		if _, err = operator.Cancel(ctx, CancelWorkRequest{ResourceID: input.ID, ActionID: uuid.New(), ReasonCode: "operator_request"}); err == nil {
			t.Fatal("foreign cancellation accepted")
		}
		var state string
		if err = f.db.QueryRow("SELECT state FROM ingestion_work_jobs WHERE id=?", mysqlstore.UUID(input.ID)).Scan(&state); err != nil || state != "available" {
			t.Fatal(state, err)
		}
	}
}
