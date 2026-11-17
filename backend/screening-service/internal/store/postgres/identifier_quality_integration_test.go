package postgres_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/internalprovider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceingest"
)

// The selection rule tests the declared engineering floors on calibration only.
// It never changes runtime settings, corpus labels, or a previously frozen run.
func qualityAcceptanceCalibration(items []qualityObservation) map[string]any {
	feasible := []int{}
	sweep := []map[string]any{}
	for threshold := 50; threshold <= 100; threshold++ {
		m := qualityMetrics(items, threshold)
		value := func(key string) float64 {
			f := m[key].(fraction)
			if f.Value == nil {
				return -1
			}
			return *f.Value
		}
		ok := value("retrieval_recall") >= .995 && value("scored_identity_recall") >= .99 && value("precision") >= .95 && value("query_false_positive_rate") >= 0 && value("query_false_positive_rate") <= .01 && value("query_false_negative_rate") >= 0 && value("query_false_negative_rate") <= .01 && m["partial_queries"].(int) == 0
		if ok {
			feasible = append(feasible, threshold)
		}
		sweep = append(sweep, map[string]any{"threshold": threshold, "meets_proposed_point_floors": ok, "metrics": m})
	}
	return map[string]any{"partition": "calibration only", "feasible_thresholds": feasible, "accepted_threshold": nil, "runtime_threshold_changed": false, "sweep": sweep, "policy": "No automatic selection: independent representative labels, confidence bounds and risk-owner approval remain mandatory; a feasible synthetic point estimate does not establish quality acceptance"}
}

func TestIdentifierProfileSeparateCalibrationAndEvaluation(t *testing.T) {
	f := newInternalFixture(t)
	ctx := context.Background()
	records := []sourcedata.Record{}
	cases := []qualityCase{}
	for i := 0; i < 90; i++ {
		entity, idType, value := "Person", "passport", fmt.Sprintf("001-A%06d", i)
		if i%3 == 1 {
			entity, idType, value = "Organization", "lei", fmt.Sprintf("%020d", 100000+i)
		}
		if i%3 == 2 {
			entity, idType, value = "Vessel", "imo", fmt.Sprintf("%07d", 1000000+i)
		}
		country := ""
		if idType == "passport" {
			country = "GH"
		}
		id := sourcedata.Identifier{Type: idType, Value: value, Country: country}
		hash := sha256.Sum256([]byte(fmt.Sprintf("identifier-corpus-family-%d", i)))
		record := sourcedata.Record{ID: fmt.Sprintf("id-family-%03d", i), Name: hex.EncodeToString(hash[:16]), Type: entity, SourceReference: "Authored fictional namespace fixture; not an official identity", Identifiers: []sourcedata.Identifier{id}}
		records = append(records, record)
		partition := []string{"development", "calibration", "holdout"}[(i/3)%3]
		add := func(stratum string, q screening.SearchQuery, relevant bool, indeterminate bool) {
			want := []string{}
			if relevant {
				want = append(want, record.ID)
			}
			cases = append(cases, qualityCase{ID: fmt.Sprintf("%s-%s", record.ID, stratum), Family: record.ID, Partition: partition, Source: f.private.Dataset, Category: "fictional_identifier_contract", Entity: entity, Stratum: stratum, Query: q, Relevant: want, Indeterminate: indeterminate})
		}
		base := screening.SearchQuery{Name: "Unrelated external spelling", Type: entity, Identifiers: []sourcedata.Identifier{id}}
		add("exact_id_unrelated_name", base, true, false)
		spaced := base
		spaced.Identifiers = []sourcedata.Identifier{id}
		spaced.Identifiers[0].Value = " " + value + " "
		add("outer_whitespace", spaced, true, false)
		altered := base
		altered.Identifiers = []sourcedata.Identifier{id}
		altered.Identifiers[0].Value = strings.TrimPrefix(value, "0")
		if altered.Identifiers[0].Value == value {
			altered.Identifiers[0].Value = value + "9"
		}
		add("different_value_unrelated_name", altered, false, false)
		wrongNamespace := base
		wrongNamespace.Identifiers = []sourcedata.Identifier{id}
		wrongNamespace.Identifiers[0].Country = "GB"
		add("different_namespace_unrelated_name", wrongNamespace, false, false)
		conflict := altered
		conflict.Name = record.Name
		add("same_name_conflicting_identifier", conflict, false, idType == "passport")
		add("exact_name_missing_identifier", screening.SearchQuery{Name: record.Name, Type: entity}, true, false)
	}
	input, e := acceptanceSnapshot(records)
	if e != nil {
		t.Fatal(e)
	}
	ingest := sourceingest.Store{DB: f.db}
	gen, e := ingest.Stage(ctx, f.private, bytes.NewReader(input))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = ingest.Publish(ctx, f.private, gen); e != nil {
		t.Fatal(e)
	}
	ref := sourcedata.DatasetRef{Scope: "tenant", ID: f.private.Dataset}
	config, _ := json.Marshal(sourcedata.Selection{Version: 2, Profile: sourcedata.IdentifierProfile, Threshold: 85, Datasets: []sourcedata.DatasetRef{ref}})
	provider := internalprovider.Provider{DB: f.db}
	observations := []qualityObservation{}
	partitions := map[string][]qualityObservation{}
	for _, q := range cases {
		request := internalRequest(q.Query.Name, q.Query.Type, ref)
		request.ProviderConfig = config
		request.Queries = []screening.SearchQuery{q.Query}
		started := time.Now()
		retrieval, e := provider.RetrieveCandidates(ctx, f.a, request)
		if e != nil {
			t.Fatal(q.ID, e)
		}
		o := qualityObservation{Case: q, Candidates: retrieval.Candidates, Partial: retrieval.Partial, Latency: float64(time.Since(started).Microseconds()) / 1000}
		observations = append(observations, o)
		partitions[q.Partition] = append(partitions[q.Partition], o)
	}
	metrics := map[string]any{}
	for p, items := range partitions {
		metrics[p] = qualityMetrics(items, 85)
	}
	scorer, e := os.ReadFile("../../matching/identifier_v1.go")
	if e != nil {
		t.Fatal(e)
	}
	scorerDigest := sha256.Sum256(scorer)
	corpus, _ := json.Marshal(struct {
		Records []sourcedata.Record
		Queries []qualityCase
	}{records, cases})
	digest := sha256.Sum256(corpus)
	acceptanceReport(t, "identifier-calibration", map[string]any{"profile": sourcedata.IdentifierProfile, "families": 90, "queries": len(cases), "partitions": metrics, "default_threshold": 85, "calibration": qualityAcceptanceCalibration(partitions["calibration"]), "diagnostics": qualityDiagnostics(observations, 85), "corpus_sha256": hex.EncodeToString(digest[:]), "scorer_sha256": hex.EncodeToString(scorerDigest[:]), "labels": "Deterministic typed-namespace contract examples; not independently adjudicated identity truth", "limitations": []string{"Artificial random unique names avoid name collisions and are not representative identity prevalence", "Same-name different-passport examples are indeterminate: people can hold multiple passports", "Missing namespace is rejected by request validation and is covered by separate unit/security tests, not labelled a true negative", "No transliteration or real official-source quality claim; no LEI/IMO checksum-validation claim", "Family partitions have distinct identifiers and names but share generation rules; no statistical independence or production acceptance claim"}, "production_quality_accepted": false, "runtime_threshold_changed": false})
	if len(cases) != 540 || len(partitions["calibration"]) != 180 || len(partitions["holdout"]) != 180 {
		t.Fatal("identifier engineering corpus coverage")
	}
}
