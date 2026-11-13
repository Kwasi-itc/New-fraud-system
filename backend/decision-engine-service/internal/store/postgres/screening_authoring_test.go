package postgres

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/google/uuid"
)

func TestScreeningAuthoringPersistenceAuthorizationAndRollback(t *testing.T) {
	db := workflowTestDB(t)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, suffix := range []string{"down", "up"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "migrations", "metadata", "000023_screening_authoring_audit."+suffix+".sql"))
		if err != nil {
			t.Fatal(err)
		}
		exec(string(raw))
	}
	tenant, scenario, inbox, user, sid, table := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec(`INSERT INTO core.tenants(id,name,schema_name,status,created_at,updated_at) VALUES($1,'Screening','screening_editor','active',now(),now())`, tenant)
	exec(`INSERT INTO core.scenarios(id,tenant_id,name,trigger_object_type,created_at,updated_at) VALUES($1,$2,'Payments','transaction',now(),now())`, scenario, tenant)
	exec(`INSERT INTO core.model_tables(id,tenant_id,name,created_at,updated_at) VALUES($1,$2,'transaction',now(),now())`, table, tenant)
	exec(`INSERT INTO core.model_fields(id,tenant_id,table_id,name,data_type,created_at,updated_at) VALUES($1,$2,$3,'beneficiary_name','string',now(),now())`, uuid.NewString(), tenant, table)
	exec(`INSERT INTO case_manager.inboxes(id,tenant_id,name,created_at,updated_at) VALUES($1,$2,'Review',now(),now())`, inbox, tenant)
	exec(`INSERT INTO identity.users(id,display_name) VALUES($1,'Synthetic administrator')`, user)
	exec(`INSERT INTO identity.memberships(tenant_id,user_id,roles) VALUES($1,$2,ARRAY['platform_admin','case_investigator'])`, tenant, user)
	exec(`INSERT INTO identity.sessions(id,digest,tenant_id,user_id,expires_at,idle_until) VALUES($1,'\x01',$2,$3,now()+interval '1 hour',now()+interval '1 hour')`, sid, tenant, user)
	exec(`INSERT INTO case_manager.inbox_users(id,tenant_id,inbox_id,user_id,created_at) VALUES($1,$2,$3,$4,now())`, uuid.NewString(), tenant, inbox, user)
	a := workflow.AuthoringActor{UserID: user, SessionID: sid, Version: 1}
	repo := ScreeningAuthoringRepository{DB: db}
	in := screening.AuthoringRule{ID: uuid.NewString(), Name: "Beneficiary", EntityType: "Person", NameField: "beneficiary_name", InboxID: inbox, Outcomes: []string{"review"}, Enabled: true}
	ignoreID := uuid.NewString()
	exec(`INSERT INTO core.custom_lists(id,tenant_id,name,description,kind,created_at,updated_at) VALUES($1,$2,'Suffixes','','generic_text',now(),now())`, ignoreID, tenant)
	exec(`INSERT INTO core.custom_list_entries(id,tenant_id,list_id,list_name,value,created_at) VALUES($1,$2,$3,'Suffixes','LTD',now())`, uuid.NewString(), tenant, ignoreID)
	listRepo := NewCustomListRepository(db)
	if _, err := listRepo.ScreeningIgnoreWords(ctx, uuid.NewString(), ignoreID); err == nil {
		t.Fatal("foreign list readable")
	}
	words, err := listRepo.ScreeningIgnoreWords(ctx, tenant, ignoreID)
	if err != nil || len(words) != 1 || words[0] != "LTD" {
		t.Fatal(words, err)
	}
	hugeList := uuid.NewString()
	exec(`INSERT INTO core.custom_lists(id,tenant_id,name,description,kind,created_at,updated_at) VALUES($1,$2,'Huge','','generic_text',now(),now())`, hugeList, tenant)
	exec(`INSERT INTO core.custom_list_entries(id,tenant_id,list_id,list_name,value,created_at) SELECT gen_random_uuid(),$1,$2,'Huge',n::text,now() FROM generate_series(1,1001) n`, tenant, hugeList)
	if _, err := listRepo.ScreeningIgnoreWords(ctx, tenant, hugeList); err == nil {
		t.Fatal("oversized list accepted")
	}
	in.IgnoreListID = uuid.NewString()
	if _, err := repo.Save(ctx, tenant, scenario, a, in); !errors.Is(err, screening.ErrAuthoringConflict) {
		t.Fatal("missing list accepted", err)
	}
	in.IgnoreListID = ignoreID
	saved, err := repo.Save(ctx, tenant, scenario, a, in)
	if err != nil {
		t.Fatal(err)
	}
	withAddress := saved
	withAddress.AddressField = "missing"
	if _, err = repo.Save(ctx, tenant, scenario, a, withAddress); !errors.Is(err, screening.ErrAuthoringConflict) {
		t.Fatal("unknown address field accepted", err)
	}
	withAddress.AddressField = in.NameField
	saved, err = repo.Save(ctx, tenant, scenario, a, withAddress)
	if err != nil || saved.AddressField != in.NameField {
		t.Fatal("address persistence", saved, err)
	}
	withBirth := saved
	withBirth.BirthDateField = "missing"
	if _, err = repo.Save(ctx, tenant, scenario, a, withBirth); !errors.Is(err, screening.ErrAuthoringConflict) {
		t.Fatal("missing DOB metadata accepted", err)
	}
	exec(`INSERT INTO core.model_fields(id,tenant_id,table_id,name,data_type,created_at,updated_at) VALUES($1,$2,$3,'event_time','timestamp',now(),now())`, uuid.NewString(), tenant, table)
	withBirth.BirthDateField = "event_time"
	if _, err = repo.Save(ctx, tenant, scenario, a, withBirth); !errors.Is(err, screening.ErrAuthoringConflict) {
		t.Fatal("timestamp DOB field accepted", err)
	}
	withBirth.BirthDateField = in.NameField
	saved, err = repo.Save(ctx, tenant, scenario, a, withBirth)
	if err != nil || saved.BirthDateField != in.NameField {
		t.Fatal("DOB persistence", saved, err)
	}

	withNationality := saved
	for _, field := range []string{"missing", "event_time"} {
		withNationality.NationalityField = field
		if _, err = repo.Save(ctx, tenant, scenario, a, withNationality); !errors.Is(err, screening.ErrAuthoringConflict) {
			t.Fatal("unsupported nationality metadata", err)
		}
	}
	withNationality.NationalityField = in.NameField
	saved, err = repo.Save(ctx, tenant, scenario, a, withNationality)
	if err != nil || saved.NationalityField != in.NameField {
		t.Fatal("nationality persistence", saved, err)
	}
	withCountry := saved
	withCountry.EntityType = "Organization"
	withCountry.BirthDateField = ""
	withCountry.NationalityField = ""
	for _, field := range []string{"missing", "event_time"} {
		withCountry.CountryField = field
		if _, err = repo.Save(ctx, tenant, scenario, a, withCountry); !errors.Is(err, screening.ErrAuthoringConflict) {
			t.Fatal("unsupported country metadata", err)
		}
	}
	withCountry.CountryField = in.NameField
	saved, err = repo.Save(ctx, tenant, scenario, a, withCountry)
	if err != nil || saved.CountryField != in.NameField {
		t.Fatal("country persistence", saved, err)
	}

	withRegistration := saved
	for _, field := range []string{"missing", "event_time"} {
		withRegistration.RegistrationField = field
		if _, err = repo.Save(ctx, tenant, scenario, a, withRegistration); !errors.Is(err, screening.ErrAuthoringConflict) {
			t.Fatal("unsupported registration metadata", err)
		}
	}
	withRegistration.RegistrationField = in.NameField
	saved, err = repo.Save(ctx, tenant, scenario, a, withRegistration)
	if err != nil || saved.RegistrationField != in.NameField {
		t.Fatal("registration persistence", saved, err)
	}

	page, err := repo.List(ctx, tenant, scenario, a)
	if err != nil || len(page.Rules) != 1 || len(page.Fields) != 1 {
		t.Fatal(page, err)
	}
	if _, err = repo.Save(ctx, tenant, scenario, a, in); !errors.Is(err, screening.ErrAuthoringConflict) {
		t.Fatal("duplicate create must not overwrite", err)
	}
	if _, err = repo.List(ctx, uuid.NewString(), scenario, a); !errors.Is(err, workflow.ErrAuthoringDenied) {
		t.Fatal("cross tenant", err)
	}
	bad := saved
	bad.InboxID = uuid.NewString()
	if _, err = repo.Save(ctx, tenant, scenario, a, bad); !errors.Is(err, workflow.ErrAuthoringDenied) {
		t.Fatal("foreign inbox", err)
	}
	bad = saved
	bad.NameField = "missing"
	if _, err = repo.Save(ctx, tenant, scenario, a, bad); !errors.Is(err, screening.ErrAuthoringConflict) {
		t.Fatal("unknown field", err)
	}
	exec(`UPDATE identity.memberships SET roles=ARRAY['platform_viewer','case_investigator'] WHERE user_id=$1`, user)
	if _, err = repo.Save(ctx, tenant, scenario, a, saved); !errors.Is(err, workflow.ErrAuthoringDenied) {
		t.Fatal("read-only actor saved", err)
	}
	exec(`UPDATE identity.memberships SET roles=ARRAY['platform_admin','case_investigator'] WHERE user_id=$1`, user)
	// Competing edits share the saved version: exactly one can commit.
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, name := range []string{"First", "Second"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			r := saved
			r.Name = name
			_, e := repo.Save(ctx, tenant, scenario, a, r)
			errs <- e
		}(name)
	}
	wg.Wait()
	close(errs)
	wins, conflicts := 0, 0
	for e := range errs {
		if e == nil {
			wins++
		} else if errors.Is(e, screening.ErrAuthoringConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatal(wins, conflicts)
	}
	page, _ = repo.List(ctx, tenant, scenario, a)
	current := page.Rules[0]
	exec(`CREATE FUNCTION core.reject_screening_authoring_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected audit failure'; END $$; CREATE TRIGGER reject_screening_authoring BEFORE INSERT ON core.screening_authoring_events FOR EACH ROW EXECUTE FUNCTION core.reject_screening_authoring_audit()`)
	changed := current
	changed.Enabled = false
	if _, err = repo.Save(ctx, tenant, scenario, a, changed); err == nil {
		t.Fatal("audit failure committed")
	}
	page, _ = repo.List(ctx, tenant, scenario, a)
	if !page.Rules[0].Enabled || !page.Rules[0].UpdatedAt.Equal(*current.UpdatedAt) {
		t.Fatal("partial write")
	}
	exec(`DROP TRIGGER reject_screening_authoring ON core.screening_authoring_events`)
	exec(`UPDATE identity.sessions SET revoked_at=now() WHERE id=$1`, sid)
	if _, err = repo.List(ctx, tenant, scenario, a); !errors.Is(err, workflow.ErrAuthoringDenied) {
		t.Fatal("revoked session", err)
	}
	exec(`UPDATE identity.sessions SET revoked_at=NULL WHERE id=$1`, sid)
	// Unsupported provider configuration must never be rewritten by this editor.
	exec(`UPDATE core.screening_configs SET config_json=config_json || '{"provider_config":{"unknown":true}}' WHERE id=$1`, in.ID)
	page, err = repo.List(ctx, tenant, scenario, a)
	if err != nil || page.OtherRules != 1 || len(page.Rules) != 0 {
		t.Fatal("legacy projection", page, err)
	}
	if _, err = repo.Save(ctx, tenant, scenario, a, current); !errors.Is(err, screening.ErrAuthoringConflict) {
		t.Fatal("unsupported overwrite", err)
	}
	down, err := os.ReadFile(filepath.Join("..", "..", "migrations", "metadata", "000023_screening_authoring_audit.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, string(down)); err == nil {
		t.Fatal("rollback discarded audit")
	}
}
