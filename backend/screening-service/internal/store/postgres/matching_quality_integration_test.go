package postgres_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/internalprovider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/matching"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceingest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type qualityCase struct {
	Indeterminate bool                  `json:"indeterminate"`
	ID            string                `json:"id"`
	Family        string                `json:"family"`
	Partition     string                `json:"partition"`
	Source        string                `json:"source"`
	Category      string                `json:"category"`
	Entity        string                `json:"entity_type"`
	Stratum       string                `json:"stratum"`
	Query         screening.SearchQuery `json:"query"`
	Relevant      []string              `json:"relevant"`
}
type qualityObservation struct {
	Case       qualityCase
	Candidates []internalprovider.RetrievedCandidate
	Partial    bool
	Latency    float64
}
type fraction struct {
	Numerator   int      `json:"numerator"`
	Denominator int      `json:"denominator"`
	Value       *float64 `json:"value"`
	Low         *float64 `json:"wilson_95_low"`
	High        *float64 `json:"wilson_95_high"`
}

func ratio(n, d int) fraction {
	f := fraction{Numerator: n, Denominator: d}
	if d == 0 {
		return f
	}
	p := float64(n) / float64(d)
	z := 1.959963984540054
	den := 1 + z*z/float64(d)
	center := (p + z*z/(2*float64(d))) / den
	half := z * math.Sqrt(p*(1-p)/float64(d)+z*z/(4*float64(d*d))) / den
	low, high := math.Max(0, center-half), math.Min(1, center+half)
	f.Value = &p
	f.Low = &low
	f.High = &high
	return f
}
func qualityMetrics(items []qualityObservation, threshold int) map[string]any {
	relevant, retrieved, tp, fp, fn, positive, negative, missed, falseQueries, partial := 0, 0, 0, 0, 0, 0, 0, 0, 0, 0
	times := []float64{}
	indeterminate := 0
	for _, item := range items {
		if item.Case.Indeterminate {
			indeterminate++
			continue
		}
		want := map[string]bool{}
		for _, id := range item.Case.Relevant {
			want[id] = true
		}
		got := map[string]bool{}
		raw := map[string]bool{}
		qualified := 0
		for _, c := range item.Candidates {
			raw[c.Ref.RecordID] = true
			if c.Evidence.Score >= float64(threshold)/100 {
				qualified++
			}
			if c.Evidence.Score >= float64(threshold)/100 && len(got) < 50 {
				got[c.Ref.RecordID] = true
			}
		}
		hit := false
		for id := range want {
			relevant++
			if raw[id] {
				retrieved++
			}
			if got[id] {
				tp++
				hit = true
			} else {
				fn++
			}
		}
		for id := range got {
			if !want[id] {
				fp++
			}
		}
		if len(want) > 0 {
			positive++
			if !hit {
				missed++
			}
		} else {
			negative++
			if len(got) > 0 {
				falseQueries++
			}
		}
		if item.Partial || qualified > 50 {
			partial++
		}
		times = append(times, item.Latency)
	}
	sort.Float64s(times)
	quantile := func(p float64) float64 {
		if len(times) == 0 {
			return 0
		}
		return times[int(math.Ceil(float64(len(times))*p))-1]
	}
	return map[string]any{"queries": len(items), "indeterminate_queries": indeterminate, "positive_queries": positive, "negative_queries": negative, "retrieval_recall": ratio(retrieved, relevant), "precision": ratio(tp, tp+fp), "scored_identity_recall": ratio(tp, tp+fn), "query_false_positive_rate": ratio(falseQueries, negative), "query_false_negative_rate": ratio(missed, positive), "partial_queries": partial, "errors": 0, "retrieval_scoring_ms": map[string]float64{"p50": quantile(.50), "p95": quantile(.95), "p99": quantile(.99)}, "threshold": threshold}
}

func TestInternalProviderLabelledMatchingQuality(t *testing.T) {
	// Random fixture tenants changed the order of equal-score candidates at the
	// output bound. A fixed test-only namespace makes the capped report repeatable
	// without changing the frozen scorer, labels, ordering or threshold policy.
	f := newInternalFixture(t, uuid.NewSHA1(uuid.NameSpaceOID, []byte("matching-quality-fixture-v1")))
	ctx := context.Background()
	p := internalprovider.Provider{DB: f.db}
	for _, pair := range [][2]string{{"foo bar", "bar foo"}, {"ama testperson", "ama testpreson"}, {"a", "b"}, {"алвена test", "алвена tests"}} {
		var similarity float64
		if err := f.db.QueryRow(ctx, `SELECT public.similarity($1,$2)`, pair[0], pair[1]).Scan(&similarity); err != nil || math.Abs(similarity-oracleTrigram(pair[0], pair[1])) > 1e-6 {
			t.Fatal("independent trigram oracle differs", pair, similarity, err)
		}
	}
	ingest := sourceingest.Store{DB: f.db}
	corpusName := "matching-corpus-v1.json"
	expectedQueries, expectedRecords := 2000, 200
	if os.Getenv("MATCHING_CORPUS_VERSION") == "2" {
		corpusName = "matching-corpus-v2.json"
		expectedQueries, expectedRecords = 4800, 240
	}
	profile := sourcedata.FuzzyProfile
	if os.Getenv("MATCHING_PROFILE") == sourcedata.TokenProfile {
		profile = sourcedata.TokenProfile
	}
	raw, err := os.ReadFile("../../../testdata/" + corpusName)
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Version int `json:"version"`
		Records []struct {
			Family    string            `json:"family"`
			Partition string            `json:"partition"`
			Source    string            `json:"source"`
			Record    sourcedata.Record `json:"record"`
		} `json:"records"`
		Queries []qualityCase `json:"queries"`
	}
	if json.Unmarshal(raw, &corpus) != nil || len(corpus.Queries) != expectedQueries || len(corpus.Records) != expectedRecords {
		t.Fatal("corpus composition")
	}
	partitionByFamily := map[string]string{}
	for _, r := range corpus.Records {
		partitionByFamily[r.Family] = r.Partition
	}
	caseIDs := map[string]bool{}
	for _, q := range corpus.Queries {
		if caseIDs[q.ID] || partitionByFamily[q.Family] != q.Partition {
			t.Fatal("duplicate query or identity leakage", q.ID)
		}
		caseIDs[q.ID] = true
	}
	keys := []sourceingest.Key{{Dataset: "quality-global"}, {Owner: f.a.Tenant(), Dataset: "quality-private"}}
	for _, key := range keys {
		records := []sourcedata.Record{}
		for _, r := range corpus.Records {
			if r.Source == key.Dataset {
				records = append(records, r.Record)
			}
		}
		if err = ingest.Register(ctx, key, "Controlled quality corpus", []uuid.UUID{f.a.Tenant()}); err != nil {
			t.Fatal(err)
		}
		input, _ := json.Marshal(map[string]any{"version": 1, "source": "controlled-fixture", "records": records})
		gen, e := ingest.Stage(ctx, key, bytes.NewReader(input))
		if e != nil {
			t.Fatal(e)
		}
		if _, e = ingest.Publish(ctx, key, gen); e != nil {
			t.Fatal(e)
		}
	}
	refs := []sourcedata.DatasetRef{{Scope: "global", ID: "quality-global"}, {Scope: "tenant", ID: "quality-private"}}
	observations := []qualityObservation{}
	counts := map[string]int{}
	oracleChecked, oracleLoss := 0, 0
	oracleStrata := map[string]int{}
	actualSearchTimes := []float64{}
	// The oracle deliberately scans this small *test* corpus, never production
	// retrieval. Its PostgreSQL keys and independent trigram set calculation are
	// compared with the indexed retrieval before threshold classification.
	for _, q := range corpus.Queries {
		if os.Getenv("QUALITY_DEVELOPMENT_ONLY") == "true" && q.Partition == "holdout" {
			continue
		}
		request := internalRequest(q.Query.Name, q.Query.Type, refs...)
		request.Queries = []screening.SearchQuery{q.Query}
		request.ProviderConfig, _ = json.Marshal(sourcedata.Selection{Version: 2, Profile: profile, Threshold: 85, Datasets: refs})
		start := time.Now()
		retrieval, e := p.RetrieveCandidates(ctx, f.a, request)
		elapsed := float64(time.Since(start).Nanoseconds()) / 1e6
		if e != nil {
			t.Fatal(q.ID, e)
		}
		observations = append(observations, qualityObservation{Case: q, Candidates: retrieval.Candidates, Partial: retrieval.Partial, Latency: elapsed})
		counts[q.Partition]++
		if q.Partition == "development" && oracleStrata[q.Stratum] < 4 {
			oracleStrata[q.Stratum]++
			oracleChecked++
			var queryKey string
			if e = f.db.QueryRow(ctx, `SELECT screening.name_key_v1($1)`, sourcedata.Normalize(q.Query.Name)).Scan(&queryKey); e != nil {
				t.Fatal(e)
			}
			got := map[string]bool{}
			for _, c := range retrieval.Candidates {
				got[c.Ref.RecordID] = true
			}
			for _, r := range corpus.Records {
				if r.Record.Type != q.Query.Type {
					continue
				}
				best := 0.0
				for _, alias := range append([]string{r.Record.Name}, r.Record.Aliases...) {
					var key string
					if e = f.db.QueryRow(ctx, `SELECT screening.name_key_v1($1)`, sourcedata.Normalize(alias)).Scan(&key); e != nil {
						t.Fatal(e)
					}
					score := matching.Score(q.Query, r.Record, alias, queryKey, key, oracleTrigram(queryKey, key))
					if profile == sourcedata.TokenProfile {
						score = matching.ScoreV2(q.Query, r.Record, alias, queryKey, key, oracleTrigram(queryKey, key))
					}
					best = math.Max(best, score.Score)
				}
				if best >= .85 && !got[r.Record.ID] {
					oracleLoss++
				}
			}
		}
		if q.Partition == "holdout" && len(actualSearchTimes) < 100 {
			start = time.Now()
			got, e := p.Search(ctx, f.a, request)
			if e != nil {
				t.Fatal(q.ID, e)
			}
			actualSearchTimes = append(actualSearchTimes, float64(time.Since(start).Nanoseconds())/1e6)
			qualified := 0
			for _, candidate := range retrieval.Candidates {
				if candidate.Evidence.Score >= .85 {
					qualified++
				}
			}
			if got.Partial != (retrieval.Partial || qualified > 50) {
				t.Fatal("provider completeness differs from retrieval/result bound")
			}
		}
	}
	// Inspect the actual runtime query, using the same local retrieval threshold.
	probe := corpus.Queries[0].Query

	probeKeys := []map[string]any{}
	for _, key := range keys {
		var generation uuid.UUID
		if err = f.db.QueryRow(ctx, `SELECT active_generation FROM screening.source_datasets WHERE owner_scope=$1 AND id=$2`, key.Owner, key.Dataset).Scan(&generation); err != nil {
			t.Fatal(err)
		}
		probeKeys = append(probeKeys, map[string]any{"owner": key.Owner, "dataset": key.Dataset, "generation": generation})
	}
	var typedKeys []internalprovider.CandidateSelection
	probeSelection, _ := json.Marshal(probeKeys)
	if err = json.Unmarshal(probeSelection, &typedKeys); err != nil {
		t.Fatal(err)
	}
	statement, planArgs, err := internalprovider.CandidateStatement(typedKeys, []screening.SearchQuery{probe})
	if err != nil {
		t.Fatal(err)
	}
	planTx, err := f.db.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer planTx.Rollback(ctx)
	if _, err = planTx.Exec(ctx, `SELECT set_config('pg_trgm.similarity_threshold','0.15',true)`); err != nil {
		t.Fatal(err)
	}
	var plan json.RawMessage
	if err = planTx.QueryRow(ctx, "EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) "+statement, planArgs...).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	if err = planTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	corpusDigest := sha256.Sum256(raw)
	matcherFile := "../../matching/name.go"
	if profile == sourcedata.TokenProfile {
		matcherFile = "../../matching/token_v2.go"
	}
	matcherBytes, err := os.ReadFile(matcherFile)
	if err != nil {
		t.Fatal(err)
	}
	matcherDigest := sha256.Sum256(matcherBytes)
	calibration := []qualityObservation{}
	for _, o := range observations {
		if o.Case.Partition == "calibration" {
			calibration = append(calibration, o)
		}
	}
	chosen, bestF1 := 85, -1.0
	sweep := []map[string]any{}
	for threshold := 50; threshold <= 99; threshold++ {
		m := qualityMetrics(calibration, threshold)
		precision := m["precision"].(fraction)
		recall := m["scored_identity_recall"].(fraction)
		f1 := 0.0
		if precision.Value != nil && recall.Value != nil && *precision.Value+*recall.Value > 0 {
			f1 = 2 * *precision.Value * *recall.Value / (*precision.Value + *recall.Value)
		}
		sweep = append(sweep, map[string]any{"threshold": threshold, "f1": f1, "metrics": m})
		if f1 > bestF1 {
			bestF1 = f1
			chosen = threshold
		}
	}
	slices := map[string][]qualityObservation{}
	for _, o := range observations {
		slices["partition:"+o.Case.Partition] = append(slices["partition:"+o.Case.Partition], o)
		if o.Case.Partition == "holdout" {
			for _, key := range []string{"stratum:" + o.Case.Stratum, "source:" + o.Case.Source, "category:" + o.Case.Category, "entity:" + o.Case.Entity} {
				slices[key] = append(slices[key], o)
			}
		}
	}
	metrics := map[string]any{}
	for key, items := range slices {
		metrics[key] = qualityMetrics(items, chosen)
	}
	sort.Float64s(actualSearchTimes)
	searchLatency := map[string]any{"samples": len(actualSearchTimes), "scope": "Full provider Search only; excludes HTTP, queue, persistence and decision evaluation"}
	if len(actualSearchTimes) > 0 {
		searchLatency["p50_ms"] = actualSearchTimes[len(actualSearchTimes)/2]
		searchLatency["p95_ms"] = actualSearchTimes[int(float64(len(actualSearchTimes))*.95)-1]
		searchLatency["p99_ms"] = actualSearchTimes[int(float64(len(actualSearchTimes))*.99)-1]
	}
	report := map[string]any{"version": corpus.Version, "profile": profile, "default_request_threshold": 85, "calibrated_threshold": chosen, "calibration_selection": "Highest calibration F1; first/lower threshold wins ties; holdout labels not used for selection", "labels": "Deterministic engineering transformations; independent two-reviewer adjudication pending", "identity_families": len(corpus.Records), "queries": len(observations), "partitions": counts, "metrics": metrics, "default_85_holdout": qualityMetrics(slices["partition:holdout"], 85), "calibration_sweep": sweep, "oracle": map[string]int{"queries": oracleChecked, "above_threshold_retrieval_losses": oracleLoss}, "provider_search_latency": searchLatency, "production_quality_claim": false, "limitations": []string{"Not official-source or independent human-labelled evidence", "Wilson intervals treat query rows as independent; shared-family correlation means they are descriptive, not institution acceptance bounds", "Combined strata/source/type cells below 50 are unassessed; no broad source/category coverage claim", "Thresholds are profile/dataset specific and do not prove identity; no automatic production threshold update"}}
	if os.Getenv("QUALITY_DIAGNOSTICS") == "true" {
		report["acceptance_constrained_calibration"] = qualityAcceptanceCalibration(calibration)
		report["classification_diagnostics"] = qualityDiagnostics(observations, chosen)
		report["default_85_diagnostics"] = qualityDiagnostics(observations, 85)
		examples := []map[string]any{}
		for _, o := range observations {
			if o.Case.Partition != "development" {
				continue
			}
			want := map[string]bool{}
			for _, id := range o.Case.Relevant {
				want[id] = true
			}
			for _, c := range o.Candidates {
				if c.Evidence.Score >= .85 && !want[c.Ref.RecordID] && len(examples) < 100 {
					examples = append(examples, map[string]any{"query_id": o.Case.ID, "query": o.Case.Query, "expected": o.Case.Relevant, "candidate": c.Record, "evidence": c.Evidence})
				}
			}
		}
		report["development_false_positive_diagnostics"] = examples
	}
	report["corpus_sha256"] = hex.EncodeToString(corpusDigest[:])
	report["scorer_sha256"] = hex.EncodeToString(matcherDigest[:])
	report["runtime_query_plan"] = plan
	if len(slices["partition:holdout"]) > 0 {
		holdout := qualityMetrics(slices["partition:holdout"], chosen)
		value := func(key string) float64 {
			f := holdout[key].(fraction)
			if f.Value == nil {
				return math.NaN()
			}
			return *f.Value
		}
		report["proposed_point_floors_met"] = holdout["partial_queries"].(int) == 0 && value("retrieval_recall") >= .995 && value("scored_identity_recall") >= .99 && value("precision") >= .95 && value("query_false_positive_rate") <= .01 && value("query_false_negative_rate") <= .01
	}
	if out := os.Getenv("MATCHING_QUALITY_REPORT"); out != "" {
		encoded, _ := json.MarshalIndent(report, "", "  ")
		if err = os.WriteFile(out, append(encoded, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("quality: %d queries, %d families, calibrated threshold %d, oracle %d queries/%d losses; no production claim", len(observations), len(corpus.Records), chosen, oracleChecked, oracleLoss)
	if len(observations) != expectedQueries && os.Getenv("QUALITY_DEVELOPMENT_ONLY") != "true" {
		t.Fatal("corpus not fully evaluated")
	}
	if oracleChecked != 40 {
		t.Fatal("oracle strata coverage", oracleChecked)
	}
}

// Independent reference implementation of the documented pg_trgm word trigrams.
func oracleTrigram(a, b string) float64 {
	grams := func(value string) map[string]bool {
		out := map[string]bool{}
		for _, word := range strings.FieldsFunc(value, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
			r := []rune("  " + word + " ")
			for i := 0; i+2 < len(r); i++ {
				out[string(r[i:i+3])] = true
			}
		}
		return out
	}
	left, right := grams(a), grams(b)
	intersection := 0
	for v := range left {
		if right[v] {
			intersection++
		}
	}
	union := len(left) + len(right) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}
