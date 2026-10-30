package postgres_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Actual local API/worker binaries and disposable PostgreSQL; only provider HTTP
// is synthetic. Worker configuration deliberately differs from API in drift mode.
func runAddressScreeningWorkers(t *testing.T, db *pgxpool.Pool, tenant, inbox, decisionURL, token string, direct bool, configs map[string]string, evaluate func(map[string]any) string) {
	t.Helper()
	binaries := map[string]string{}
	for _, key := range []string{"SCREENING_INTEGRATION_BINARY", "CASE_MANAGER_INTEGRATION_BINARY", "DECISION_WORKER_INTEGRATION_BINARY", "SCREENING_WORKER_INTEGRATION_BINARY"} {
		binaries[key] = os.Getenv(key)
		if binaries[key] == "" {
			t.Skip("address worker test requires " + key)
		}
	}
	ctx := context.Background()
	type query struct {
		Schema     string              `json:"schema"`
		Properties map[string][]string `json:"properties"`
	}
	var mu sync.Mutex
	observed := map[string][]query{}
	genericCalls := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/match/") || r.Method != "POST" {
			mu.Lock()
			genericCalls++
			mu.Unlock()
			http.Error(w, "unexpected adapter request", 400)
			return
		}

		var body struct {
			Queries map[string]query `json:"queries"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || (len(body.Queries) < 1 || len(body.Queries) > 2) {
			t.Error("invalid direct payload")
			http.Error(w, "invalid request", 400)
			return
		}
		q := body.Queries["q1"]
		if len(q.Properties["name"]) != 1 {
			t.Error("missing mapped name")
			http.Error(w, "invalid name", 400)
			return
		}
		name := q.Properties["name"][0]
		if name == "Synthetic Recognized Input" {
			extra := body.Queries["q2"]
			if len(body.Queries) != 2 || extra.Schema != "Person" || len(extra.Properties) != 1 || len(extra.Properties["name"]) != 1 || extra.Properties["name"][0] != "Synthetic Extracted" {
				t.Error("recognized query not serialized", extra)
			}
		} else if len(body.Queries) != 1 {
			t.Error("unexpected extra query")
		}
		if name != "Synthetic Address" && (r.URL.Query().Get("threshold") != "0.85" || r.URL.Query().Get("cutoff") != "0.85" || r.URL.Query().Get("limit") != "7" || strings.Join(r.URL.Query()["include_dataset"], ",") != "synthetic,other-test") {
			t.Error("saved matching settings missing from provider request", r.URL.RawQuery)
		}
		mu.Lock()
		observed[name+":"+q.Schema] = append(observed[name+":"+q.Schema], q)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		responses := map[string]any{"q1": map[string]any{"total": map[string]int{"value": 1}, "results": []any{map[string]any{"id": "synthetic-address-entity", "caption": "Synthetic match", "schema": q.Schema, "score": 0.95, "match": true, "datasets": []string{"synthetic"}, "properties": map[string][]string{"name": {"Synthetic match"}}}}}}
		if len(body.Queries) == 2 {
			responses["q2"] = map[string]any{"total": map[string]int{"value": 0}, "results": []any{}}
		}
		json.NewEncoder(w).Encode(map[string]any{"responses": responses})
	}))
	t.Cleanup(provider.Close)
	const owner = "synthetic-address-owner-private-token-32"
	screeningURL, _, _ := startInvestigatorServer(t, db, binaries["SCREENING_INTEGRATION_BINARY"], tenant, "SCREENING_OPERATOR_TOKEN="+owner, "OPENSANCTIONS_API_HOST="+provider.URL, "OPENSANCTIONS_AUTH_MODE=none", "OPENSANCTIONS_API_KEY=", "SCREENING_PROVIDER_URL=", "SCREENING_PROVIDER_URLS=")
	caseURL, _ := startCaseServer(t, db, binaries["CASE_MANAGER_INTEGRATION_BINARY"], tenant)
	env := []string{"CASE_SERVICE_URL=" + caseURL, "CASE_SERVICE_AUTH_TOKEN=" + token, "DECISION_ENGINE_URL=" + decisionURL, "HTTP_CLIENT_TIMEOUT=2s"}
	if direct {
		env = append(env, "OPENSANCTIONS_API_HOST="+provider.URL, "OPENSANCTIONS_AUTH_MODE=none")
	} else {
		env = append(env, "SCREENING_PROVIDER_URL="+provider.URL)
	}
	startJourneyWorker(t, db, binaries["SCREENING_WORKER_INTEGRATION_BINARY"], env...)
	startJourneyWorker(t, db, binaries["DECISION_WORKER_INTEGRATION_BINARY"], "SCREENING_SERVICE_URL="+screeningURL)
	client := &http.Client{Timeout: 5 * time.Second}
	httpRequest := func(method, path, bearer string, body []byte, want int) []byte {
		t.Helper()
		req, err := http.NewRequest(method, screeningURL+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Set("Content-Type", "application/json")
		if strings.HasPrefix(path, "/v1/") {
			req.Header.Set("X-Screening-Operator-Token", owner)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		if err != nil || resp.StatusCode != want {
			t.Fatalf("%s %s got %d want %d: %s %v", method, path, resp.StatusCode, want, raw, err)
		}
		return raw
	}
	var cap struct {
		Address bool `json:"address_supported"`
	}
	if json.Unmarshal(httpRequest("GET", "/internal/screening-capabilities", testIntakeToken, nil, 200), &cap) != nil || !cap.Address {
		t.Fatal("API must advertise direct capability, including worker drift test")
	}
	cases := []struct {
		name    string
		value   any
		present bool
		want    string
	}{
		{"Synthetic Address Populated", "  Synthetic Street  ", true, "Synthetic Street"},
		{"Synthetic Address Missing", nil, false, ""},
		{"Synthetic Address Null", nil, true, ""},
		{"Synthetic Address Blank", "   ", true, ""},
		{"Synthetic Birth Year", "", true, ""},
		{"Synthetic Recognized Input", "", true, ""},
	}
	if !direct {
		cases = cases[:1]
		cases[0].name = "Synthetic Address Drift"
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fields := map[string]any{"beneficiary_name": "123" + tc.name + "456 LTD"}
			if tc.present {
				fields["other"] = tc.value
			}
			birth := ""
			switch tc.name {
			case "Synthetic Address Populated", "Synthetic Address Drift":
				birth = "2000-02-29"
				fields["birth_text"] = "  " + birth + "  "
			case "Synthetic Birth Year":
				birth = "1980"
				fields["birth_text"] = birth
			case "Synthetic Address Null":
				fields["birth_text"] = nil
			case "Synthetic Address Blank":
				fields["birth_text"] = "   "
			}
			nationality, country := "", ""
			switch tc.name {
			case "Synthetic Address Populated", "Synthetic Address Drift", "Synthetic Birth Year":
				nationality, country = "gb", "us"
				fields["nationality_text"] = " GB "
				fields["country_text"] = " US "
			case "Synthetic Address Null":
				fields["nationality_text"] = nil
				fields["country_text"] = nil
			case "Synthetic Address Blank":
				fields["nationality_text"] = "   "
				fields["country_text"] = "   "
			}
			registration := ""
			switch tc.name {
			case "Synthetic Address Populated", "Synthetic Address Drift", "Synthetic Birth Year":
				registration = "001-Ab/09"
				fields["registration_text"] = "  " + registration + "  "
			case "Synthetic Address Null":
				fields["registration_text"] = nil
			case "Synthetic Address Blank":
				fields["registration_text"] = "   "
			}
			decision := evaluate(fields)
			for config, entity := range configs {
				var source, execution string
				expected := "completed"
				sourceStatus := "awaiting_review"
				if !direct {
					expected = "failed"
					sourceStatus = "failed"
				}
				waitFor(t, func() bool {
					return db.QueryRow(ctx, `SELECT s.id::text,e.id::text FROM core.screening_executions e JOIN screening.screenings s ON s.tenant_id=e.tenant_id AND s.idempotency_key=e.id::text WHERE e.decision_id=$1 AND e.config_id=$2 AND e.tenant_id=$3 AND e.status=$4 AND s.status=$5 AND e.projection_source_id=s.id`, decision, config, tenant, expected, sourceStatus).Scan(&source, &execution) == nil
				})
				var raw []byte
				if err := db.QueryRow(ctx, `SELECT request_json FROM screening.screenings WHERE id=$1 AND tenant_id=$2`, source, tenant).Scan(&raw); err != nil {
					t.Fatal(err)
				}
				var request struct {
					Queries []struct {
						Name, Type, Address, Nationality, Country string
						RegistrationNumber                        string `json:"registration_number"`
						BirthDate                                 string `json:"birth_date"`
					} `json:"queries"`
				}
				expectedQueries := 1
				if tc.name == "Synthetic Recognized Input" {
					expectedQueries = 2
				}
				if json.Unmarshal(raw, &request) != nil || len(request.Queries) != expectedQueries || request.Queries[0].Name != tc.name || request.Queries[0].Type != entity || request.Queries[0].Address != tc.want {
					t.Fatal("source query changed", string(raw))
				}
				expectedBirth := birth
				if entity != "Person" {
					expectedBirth = ""
				}
				if request.Queries[0].BirthDate != expectedBirth {
					t.Fatal("source DOB changed", request.Queries[0].BirthDate, expectedBirth)
				}

				wantNationality, wantCountry := nationality, country
				if entity == "Person" {
					wantCountry = ""
				} else {
					wantNationality = ""
				}
				if request.Queries[0].Nationality != wantNationality || request.Queries[0].Country != wantCountry {
					t.Fatal("stored country identity mismatch", request.Queries[0])
				}

				wantRegistration := registration
				if entity != "Organization" {
					wantRegistration = ""
				}
				if request.Queries[0].RegistrationNumber != wantRegistration {
					t.Fatal("stored registration changed", request.Queries[0])
				}

				if direct {
					waitFor(t, func() bool {
						var n int
						return db.QueryRow(ctx, `SELECT count(*) FROM case_manager.case_screenings cs JOIN case_manager.cases c ON c.id=cs.case_id AND c.tenant_id=cs.tenant_id WHERE cs.screening_id=$1 AND c.tenant_id=$2 AND c.inbox_id=$3`, source, tenant, inbox).Scan(&n) == nil && n == 1
					})
					var matches int
					if err := db.QueryRow(ctx, `SELECT count(*) FROM screening.screening_matches WHERE screening_id=$1 AND tenant_id=$2 AND entity_id='synthetic-address-entity' AND provider='opensanctions' AND score=0.95 AND payload->'datasets'->>0='synthetic' AND payload->>'schema'=$3`, source, tenant, entity).Scan(&matches); err != nil || matches != 1 {
						t.Fatal("candidate linkage", matches, err)
					}
					mu.Lock()
					seen := append([]query(nil), observed[tc.name+":"+entity]...)
					mu.Unlock()
					if len(seen) != 1 {
						t.Fatal("wrong provider call count", len(seen))
					}
					for key, want := range map[string]string{"nationality": wantNationality, "country": wantCountry, "registrationNumber": wantRegistration} {
						values := seen[0].Properties[key]
						if want == "" {
							if len(values) != 0 {
								t.Fatal("unexpected identity property", key, values)
							}
						} else if len(values) != 1 || values[0] != want {
							t.Fatal("country identity provider mismatch", key, values, want)
						}
					}
					date := seen[0].Properties["birthDate"]
					if expectedBirth == "" {
						if len(date) != 0 {
							t.Fatal("unexpected provider DOB", date)
						}
					} else if len(date) != 1 || date[0] != expectedBirth {
						t.Fatal("provider DOB mismatch", date, expectedBirth)
					}
					address := seen[0].Properties["address"]
					if tc.want == "" {
						if len(address) != 0 {
							t.Fatal("blank address serialized", address)
						}
					} else if len(address) != 1 || address[0] != tc.want {
						t.Fatal("address missing from provider", address)
					}
				} else {
					var failure string
					if err := db.QueryRow(ctx, `SELECT last_error FROM screening.screenings WHERE id=$1`, source).Scan(&failure); err != nil || !strings.Contains(failure, "advanced search requires direct OpenSanctions") || strings.Contains(failure, tc.name) || strings.Contains(failure, provider.URL) {
						t.Fatal("missing/unsafe drift failure", failure, err)
					}
					httpRequest("POST", "/v1/tenants/"+tenant+"/screenings/"+source+"/retry", token, nil, 200)
					waitFor(t, func() bool {
						var n int
						return db.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE kind='screening_dispatch' AND args->>'screening_id'=$1 AND state IN ('completed','discarded')`, source).Scan(&n) == nil && n == 2
					})
					var status string
					var matches, links int
					if err := db.QueryRow(ctx, `SELECT status,(SELECT count(*) FROM screening.screening_matches WHERE screening_id=$1),(SELECT count(*) FROM case_manager.case_screenings WHERE screening_id=$1) FROM screening.screenings WHERE id=$1`, source).Scan(&status, &matches, &links); err != nil || status != "failed" || matches != 0 || links != 0 {
						t.Fatal("drift retry produced inconsistent result", status, matches, links, err)
					}
				}
				// Authorized public retrieval, foreign tenant denial and duplicate intake.
				for i := 0; i < 3; i++ {
					httpRequest("GET", "/v1/tenants/"+tenant+"/screenings/"+source, token, nil, 200)
				}
				httpRequest("GET", "/v1/tenants/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa/screenings/"+source, token, nil, 404)
				if err := db.QueryRow(ctx, `SELECT request_json FROM core.screening_executions WHERE id=$1 AND tenant_id=$2`, execution, tenant).Scan(&raw); err != nil {
					t.Fatal(err)
				}
				httpRequest("POST", "/internal/v1/tenants/"+tenant+"/decision-screenings", testIntakeToken, raw, 201)
				var sources, jobs int
				wantJobs := 1
				if !direct {
					wantJobs = 2
				}
				if err := db.QueryRow(ctx, `SELECT (SELECT count(*) FROM screening.screenings WHERE tenant_id=$1 AND idempotency_key=$2),(SELECT count(*) FROM river_job WHERE kind='screening_dispatch' AND args->>'screening_id'=$3)`, tenant, execution, source).Scan(&sources, &jobs); err != nil || sources != 1 || jobs != wantJobs {
					t.Fatal("replay duplicated source/job", sources, jobs, err)
				}
			}
			var outcome string
			if err := db.QueryRow(ctx, `SELECT outcome FROM core.decisions WHERE id=$1 AND tenant_id=$2`, decision, tenant).Scan(&outcome); err != nil || outcome != "approve" {
				t.Fatal("unexpected decision change", outcome, err)
			}
		})
	}
	mu.Lock()
	defer mu.Unlock()
	if genericCalls != 0 {
		t.Fatal("unsupported address reached generic provider", genericCalls)
	}
	if !direct && len(observed) != 0 {
		t.Fatal("drift worker unexpectedly used direct adapter")
	}
}
