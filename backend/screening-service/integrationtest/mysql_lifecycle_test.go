package integrationtest

import (
	"errors"
	identity "github.com/Kwasi-itc/New-fraud-system/backend/investigator"
	d "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	api "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/httpapi"
	runtime "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/mysqlruntime"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/service"
	native "github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/store/mysql"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"sync"
	"testing"
	"time"
)

type clock struct{}

func (clock) Now() time.Time { return time.Now().UTC() }
func fixtureService(f *fixture) service.ScreeningService {
	return api.MySQLService(f.store, api.MySQLDependencies{Provider: runtime.LocalProvider{}})
}
func (f *fixture) principal() identity.Principal {
	return identity.Principal{Subject: f.user.String(), TenantID: f.tenant, SessionID: f.sid, AuthzVersion: 1, Admin: true}
}
func (f *fixture) manualInput() service.ManualSearchInput {
	return service.ManualSearchInput{OperationID: uuid.NewString(), Name: "Alex Example", EntityType: "Person", Threshold: 90, Limit: 10}
}
func TestMySQLScreeningManualDispatchAndProvenance(t *testing.T) {
	f := fixtureMySQL(t)
	svc := fixtureService(f)
	ctx := native.WithPrincipal(f.ctx, f.principal())
	input := f.manualInput()
	created, e := svc.CreateManualSearch(ctx, f.tenant.String(), f.user.String(), input)
	must(t, e)
	replay, e := svc.CreateManualSearch(ctx, f.tenant.String(), f.user.String(), input)
	must(t, e)
	if replay.ID != created.ID {
		t.Fatal("manual replay duplicated screening")
	}
	changed := input
	changed.Name = "Morgan Sample"
	_, e = svc.CreateManualSearch(ctx, f.tenant.String(), f.user.String(), changed)
	if !errors.Is(e, service.ErrManualConflict) {
		t.Fatalf("conflicting replay accepted: %v", e)
	}
	_, e = f.store.Screenings().GetByID(ctx, f.other.String(), created.ID)
	if !errors.Is(e, pgx.ErrNoRows) {
		t.Fatalf("cross-tenant source visible: %v", e)
	}
	dispatch := service.NewDispatchService(f.store, clock{}, f.store.Screenings(), f.store.ScreeningMatches(), runtime.LocalProvider{}, nil)
	must(t, dispatch.RunScreening(f.ctx, f.tenant.String(), created.ID))
	details, e := svc.GetManualSearch(ctx, f.tenant.String(), f.user.String(), created.ID)
	must(t, e)
	if len(details.Matches) != 1 || details.Screening.Status != "awaiting_review" || details.Matches[0].EntityID != "local-person-001" {
		t.Fatalf("unexpected stored synthetic result: %+v", details)
	}
	if _, e = uuid.Parse(details.Matches[0].ID); e != nil {
		t.Fatal("native match identifier is not a UUID")
	}
	page, e := svc.ListManualSearches(ctx, f.tenant.String(), f.user.String(), d.ManualHistoryFilter{Limit: 20})
	must(t, e)
	if len(page.Items) != 1 || page.Items[0].ID != created.ID {
		t.Fatal("manual owner history missing")
	}
	var creator string
	must(t, f.db.QueryRowContext(f.ctx, "SELECT BIN_TO_UUID(creator_id) FROM `"+f.database+"`.screening_screenings WHERE id=UUID_TO_BIN(?)", created.ID).Scan(&creator))
	if creator != f.user.String() {
		t.Fatal("authoritative manual creator missing")
	}
	before, _ := svc.GetDetails(ctx, f.tenant.String(), created.ID)
	rollback := errors.New("injected rollback")
	e = f.store.Run(ctx, func(m ports.MutationStore) error {
		v := before.Matches[0]
		v.Status = "no_hit"
		_, e := m.ScreeningMatches().Update(ctx, v)
		if e != nil {
			return e
		}
		return rollback
	})
	if !errors.Is(e, rollback) {
		t.Fatal(e)
	}
	after, e := svc.GetDetails(ctx, f.tenant.String(), created.ID)
	must(t, e)
	if after.Matches[0].Status != "pending" {
		t.Fatal("failed mutation escaped rollback")
	}
}
func TestMySQLScreeningConcurrentManualReplayAndRevocation(t *testing.T) {
	f := fixtureMySQL(t)
	svc := fixtureService(f)
	ctx := native.WithPrincipal(f.ctx, f.principal())
	input := f.manualInput()
	var wg sync.WaitGroup
	ids := make(chan string, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := svc.CreateManualSearch(ctx, f.tenant.String(), f.user.String(), input)
			ids <- v.ID
			errs <- e
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for e := range errs {
		must(t, e)
	}
	first := ""
	for id := range ids {
		if first != "" && id != first {
			t.Fatal("concurrent intake duplicated work")
		}
		first = id
	}
	_, e := f.db.ExecContext(f.ctx, "UPDATE `"+f.database+"`.identity_sessions SET revoked_at=UTC_TIMESTAMP(6) WHERE id=UUID_TO_BIN(?)", f.sid.String())
	must(t, e)
	_, e = svc.CreateManualSearch(ctx, f.tenant.String(), f.user.String(), f.manualInput())
	if e == nil {
		t.Fatal("revoked session mutated screening")
	}
}
func TestMySQLScreeningClaimFencingAndCallbackReplay(t *testing.T) {
	f := fixtureMySQL(t)
	svc := fixtureService(f)
	v, e := svc.CreateManualSearch(f.ctx, f.tenant.String(), f.user.String(), f.manualInput())
	must(t, e)
	one, two := uuid.NewString(), uuid.NewString()
	ok, e := f.store.Screenings().Claim(f.ctx, f.tenant.String(), v.ID, one, time.Now().UTC())
	must(t, e)
	if !ok {
		t.Fatal("initial claim failed")
	}
	ok, e = f.store.Screenings().Claim(f.ctx, f.tenant.String(), v.ID, two, time.Now().UTC())
	must(t, e)
	if ok {
		t.Fatal("live claim stolen")
	}
	_, e = f.db.ExecContext(f.ctx, "UPDATE `"+f.database+"`.screening_screenings SET processing_until=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)),projection_version=projection_version+1 WHERE id=UUID_TO_BIN(?)", v.ID)
	must(t, e)
	e = f.store.Run(f.ctx, func(m ports.MutationStore) error {
		return m.Screenings().LockClaim(f.ctx, f.tenant.String(), v.ID, one)
	})
	if !errors.Is(e, ports.ErrScreeningBusy) {
		t.Fatal("expired claimant retained publication authority")
	}
	ok, e = f.store.Screenings().Claim(f.ctx, f.tenant.String(), v.ID, two, time.Now().UTC())
	must(t, e)
	if !ok {
		t.Fatal("expired claim was not recovered")
	}
	event := uuid.NewString()
	payload := ports.ScreeningRequiresReviewCommand{Version: 1, EventID: event, TenantID: f.tenant.String(), ScreeningID: v.ID}
	must(t, f.store.Run(f.ctx, func(m ports.MutationStore) error {
		return m.CaseEvents().Enqueue(f.ctx, event, f.tenant.String(), "requires-review", payload)
	}))
	delivery, e := f.store.Claim(f.ctx)
	must(t, e)
	if delivery == nil {
		t.Fatal("missing pending callback")
	}
	must(t, f.store.Finish(f.ctx, *delivery, nil))
	must(t, f.store.ReplayCallback(f.ctx, f.tenant.String(), event))
	replay, e := f.store.Claim(f.ctx)
	must(t, e)
	if replay == nil || replay.ID != delivery.ID || replay.LeaseToken == delivery.LeaseToken {
		t.Fatal("replay identity/fence incorrect")
	}
	if e = f.store.Finish(f.ctx, *delivery, nil); !errors.Is(e, ports.ErrScreeningBusy) {
		t.Fatal("stale delivery acknowledged newer claim")
	}
	must(t, f.store.Finish(f.ctx, *replay, nil))
}
