// Package integrationtest supplies cross-owner acceptance journeys for generated
// disposable databases. It is never imported by application startup.
package integrationtest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/decisionaccess"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqlapi"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqlexecution"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/mysqlruntime"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/service"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/store/mysql"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/testsupport"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type IDs struct{}

func (IDs) New() uuid.UUID { return uuid.New() }

type Clock struct{}

func (Clock) Now() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

type Preparation struct {
	t               *testing.T
	db              *sql.DB
	database        string
	tenant, table   uuid.UUID
	http            http.Handler
	path, iteration string
	owner           *decisionaccess.Owner
	reader          mysql.TenantDataReader
	executions      *mysql.ExecutionStore
	authorize       mysql.AuthoringAuthorizer
	session         mysql.TrustedDecisionSession
}

// Begin creates and starts a real decision HTTP preparation against the supplied
// owner database. The caller processes the returned durable owner job, then calls
// Finish to verify status, commit and publication through the same HTTP routes.
func Begin(t *testing.T, db *sql.DB, database string, tenant, table uuid.UUID, tableName, column string) (*Preparation, uuid.UUID) {
	t.Helper()
	// The surrounding DDL fixture deliberately has a one-connection pool.
	// Real overlap tests need independent sessions, including a lock observer.
	db.SetMaxOpenConns(8)
	testsupport.InstallDecisionOnOwnerFixture(t, db, database)
	testsupport.InstallDecisionAccessOwners(t, db, database)
	schema, _ := mysqlstore.Identifier(database)
	ctx := context.Background()
	actor, sid := uuid.New(), uuid.New()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO "+schema+".identity_tenants VALUES(?,TRUE)", mysqlstore.UUID(tenant))
	exec("INSERT INTO "+schema+".identity_users VALUES(?,'preparation author',TRUE)", mysqlstore.UUID(actor))
	exec("INSERT INTO "+schema+".identity_memberships(tenant_id,user_id,roles,active) VALUES(?,?,'platform_admin,case_investigator',TRUE)", mysqlstore.UUID(tenant), mysqlstore.UUID(actor))
	exec("INSERT INTO "+schema+".identity_sessions(id,digest,tenant_id,user_id,created_at,expires_at,idle_until) VALUES(?,UNHEX(SHA2('preparation-session',256)),?,?,SYSDATE(6),SYSDATE(6)+INTERVAL 1 HOUR,SYSDATE(6)+INTERVAL 30 MINUTE)", mysqlstore.UUID(sid), mysqlstore.UUID(tenant), mysqlstore.UUID(actor))
	auth, err := mysql.SessionAuthorizer(database, mysql.TrustedDecisionSession{ActorID: actor, TenantID: tenant, SessionID: sid, MembershipVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	read := func(c context.Context, tx *sql.Tx, id uuid.UUID) error { return auth(c, tx, id, mysql.AuthoringRead) }
	write := func(c context.Context, tx *sql.Tx, id uuid.UUID) error { return auth(c, tx, id, mysql.AuthoringWrite) }
	owner, err := decisionaccess.New(decisionaccess.Config{DB: db, Database: database, Tenant: tenant, MaxRows: 500, MaxBytes: 2 << 20, Queue: "native_worker", MaxAttempts: 5, Lease: 5 * time.Minute, Read: read, Write: write, IDs: IDs{}, Clock: Clock{}})
	if err != nil {
		t.Fatal(err)
	}
	deps, err := mysqlruntime.BindModelOwner(mysqlruntime.Dependencies{DB: db, Database: database}, owner)
	if err != nil {
		t.Fatal(err)
	}
	authoring, err := mysql.NewAuthoringStore(db, database, tenant, 500, 2<<20, auth)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := mysql.NewTenantDataReader(db, database, tenant, 500, 2<<20, deps.Models, func(c context.Context, tx *sql.Tx, id uuid.UUID, write bool) error {
		if write {
			return mysql.ErrDecisionAuthorizationDenied
		}
		return read(c, tx, id)
	})
	if err != nil {
		t.Fatal(err)
	}
	limits := service.ExecutionPreparationLimits{MaxItems: 500, MaxBytes: 2 << 20}
	pub, err := mysqlexecution.NewPublication(authoring, reader, limits)
	if err != nil {
		t.Fatal(err)
	}
	pub = pub.WithIndexPreparation(deps.IndexPreparation)
	admin, err := mysqlexecution.NewAuthoring(authoring, reader, IDs{}, Clock{}, limits)
	if err != nil {
		t.Fatal(err)
	}
	policy := mysql.ExecutionPolicy{CallbackMaxAttempts: 3, Queues: map[ports.JobKind]mysql.DeliveryQueuePolicy{}}
	for _, kind := range []ports.JobKind{ports.JobScheduled, ports.JobAsync, ports.JobCallback} {
		policy.Queues[kind] = mysql.DeliveryQueuePolicy{Name: string(kind), MaxAttempts: 3}
	}
	executions, err := mysql.NewExecutionStore(db, database, tenant, 500, 2<<20, func(c context.Context, tx *sql.Tx, id uuid.UUID, a mysql.ExecutionAction) error {
		if a == mysql.ExecutionRequest {
			return write(c, tx, id)
		}
		if a != mysql.ExecutionRead {
			return mysql.ErrDecisionAuthorizationDenied
		}
		return read(c, tx, id)
	}, policy)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := mysqlapi.NewExecutionAdapter(executions)
	if err != nil {
		t.Fatal(err)
	}
	h, err := mysqlapi.NewExecutionHTTP(func(c context.Context, id uuid.UUID) (*mysqlapi.ExecutionEndpointScope, error) {
		if id != tenant {
			return nil, mysql.ErrTenantScope
		}
		return &mysqlapi.ExecutionEndpointScope{Read: adapter, Publication: pub, Authoring: admin, Clock: Clock{}}, nil
	}, 2<<20)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	h.Register(router.Group("/v1"))
	p := &Preparation{t: t, db: db, database: database, tenant: tenant, table: table, http: router, owner: owner, reader: reader, executions: executions, authorize: auth, session: mysql.TrustedDecisionSession{ActorID: actor, TenantID: tenant, SessionID: sid, MembershipVersion: 1}}
	base := "/v1/tenants/" + tenant.String() + "/scenarios"
	id := p.id(p.request("POST", base, fmt.Sprintf(`{"name":"Physical journey","trigger_object_type":%q}`, tableName), 201), "scenario")
	p.path = base + "/" + id
	p.iteration = p.id(p.request("POST", p.path+"/iterations", "", 201), "iteration")
	ip := p.path + "/iterations/" + p.iteration
	p.request("PUT", ip, `{"trigger_formula":{"constant":true},"score_review_threshold":1,"score_block_and_review_threshold":10,"score_decline_threshold":20}`, 200)
	formula := fmt.Sprintf(`{"function":"gt","children":[{"function":"Aggregator","named_children":{"tableName":{"constant":%q},"fieldName":{"constant":%q},"aggregator":{"constant":"SUM"},"filters":{"function":"List","children":[{"function":"Filter","named_children":{"tableName":{"constant":%q},"fieldName":{"constant":%q},"operator":{"constant":">"},"value":{"constant":0}}}]}}},{"constant":0}]}`, tableName, column, tableName, column)
	p.request("POST", ip+"/rules", `{"name":"Indexed aggregate","formula":`+formula+`,"score_modifier":1,"rule_group":"risk","stable_rule_id":"physical"}`, 201)
	responses := make(chan *httptest.ResponseRecorder, 2)
	start := make(chan struct{})
	for range 2 {
		go func() {
			<-start
			rec := httptest.NewRecorder()
			p.http.ServeHTTP(rec, httptest.NewRequest("POST", p.path+"/publications/preparation", strings.NewReader(`{"iteration_id":"`+p.iteration+`"}`)))
			responses <- rec
		}()
	}
	close(start)
	for range 2 {
		rec := <-responses
		if rec.Code != 202 {
			t.Fatalf("simultaneous preparation: %d %s", rec.Code, rec.Body.String())
		}
	}
	jobs, err := owner.List(ctx, tenant.String(), []string{table.String()}, 500)
	if err != nil || len(jobs) != 1 {
		t.Fatal(jobs, err)
	}
	return p, uuid.MustParse(jobs[0].ID)
}

// RetryAfterFailure simulates a caller losing the accepted retry response and
// restarting preparation. The durable job identity must remain unique.
func (p *Preparation) RetryAfterFailure() {
	p.t.Helper()
	raw := p.request("GET", p.path+"/publications/preparation?iteration_id="+p.iteration, "", 200)
	var state struct {
		Preparation struct {
			Finished bool `json:"preparation_finished"`
		} `json:"preparation"`
	}
	if err := json.Unmarshal(raw, &state); err != nil || state.Preparation.Finished {
		p.t.Fatal("failed job reported ready", string(raw), err)
	}
	p.request("POST", p.path+"/publications/preparation", `{"iteration_id":"`+p.iteration+`"}`, 202) // accepted response discarded
	p.request("POST", p.path+"/publications/preparation", `{"iteration_id":"`+p.iteration+`"}`, 202)
	jobs, err := p.owner.List(context.Background(), p.tenant.String(), []string{p.table.String()}, 500)
	if err != nil || len(jobs) != 1 || jobs[0].Status != "pending" {
		p.t.Fatal("retry duplicated or lost job", jobs, err)
	}
}
func (p *Preparation) request(method, path, body string, want int) []byte {
	p.t.Helper()
	rec := httptest.NewRecorder()
	p.http.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	if rec.Code != want {
		p.t.Fatalf("%s %s: %d want %d: %s", method, path, rec.Code, want, rec.Body.String())
	}
	return rec.Body.Bytes()
}
func (p *Preparation) id(raw []byte, key string) string {
	p.t.Helper()
	var out map[string]struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out[key].ID == "" {
		p.t.Fatal(string(raw), err)
	}
	return out[key].ID
}
func (p *Preparation) Finish() {
	p.t.Helper()
	raw := p.request("GET", p.path+"/publications/preparation?iteration_id="+p.iteration, "", 200)
	var out struct {
		Preparation struct {
			Finished bool `json:"preparation_finished"`
			Required bool `json:"preparation_required"`
		} `json:"preparation"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || !out.Preparation.Finished || out.Preparation.Required {
		p.t.Fatal("physical preparation not ready", string(raw), err)
	}
	p.request("POST", p.path+"/iterations/"+p.iteration+"/commit", "{}", 200)
	p.request("POST", p.path+"/publications", `{"action":"publish","iteration_id":"`+p.iteration+`"}`, 200)
}
