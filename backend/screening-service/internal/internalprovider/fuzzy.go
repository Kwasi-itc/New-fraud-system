package internalprovider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/matching"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/provideraccess"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const CandidateBound = 2000

// CandidateQuerySQL is shared with the isolated EXPLAIN verification harness.
// The lateral LIMIT keeps the indexed name predicate ahead of the record join.
// A row outside a group's top K cannot enter the global top K; this preserves
// the existing candidate ordering/bound while avoiding a full record join for
// each search. Raw XML is restricted ingestion evidence, never scoring input.
const CandidateQuerySQL = `WITH queries AS MATERIALIZED(SELECT screening.name_key_v1(name) AS key,type,query FROM jsonb_to_recordset($2::jsonb) AS j(name text,type text,query jsonb))
 SELECT c.owner_scope,c.dataset_id,c.generation,c.record_id,c.entity_id,c.record,c.original,c.fuzzy_name,q.key,q.query,c.similarity
 FROM jsonb_to_recordset($1::jsonb) AS d(owner uuid,dataset text,generation uuid)
 CROSS JOIN queries q
 CROSS JOIN LATERAL (
 SELECT r.owner_scope,r.dataset_id,r.generation,r.record_id,r.entity_id,r.record-'original_xml' AS record,n.original,n.fuzzy_name,public.similarity(n.fuzzy_name,q.key) AS similarity
 FROM screening.source_names n
 JOIN screening.source_records r ON(r.owner_scope,r.dataset_id,r.generation,r.record_id)=(n.owner_scope,n.dataset_id,n.generation,n.record_id)
 WHERE(n.owner_scope,n.dataset_id,n.generation)=(d.owner,d.dataset,d.generation) AND n.fuzzy_name OPERATOR(public.%) q.key AND r.entity_type=q.type
 ORDER BY public.similarity(n.fuzzy_name,q.key) DESC,r.entity_id,n.original LIMIT $3
 ) c
 ORDER BY c.similarity DESC,c.entity_id,c.original,q.key LIMIT $3`

type CandidateSelection struct {
	Owner      uuid.UUID `json:"owner"`
	Dataset    string    `json:"dataset"`
	Generation uuid.UUID `json:"generation"`
}

// CandidateStatement preserves the batched query while giving PostgreSQL the
// actual bounded VALUES cardinalities and typed parameters. JSON recordset's
// default 100-row estimates caused 40,000-row scope scans and JIT per request.
// Only generated placeholder numbers enter SQL; all source/query values bind.
func CandidateStatement(keys []CandidateSelection, queries []screening.SearchQuery) (string, []any, error) {
	if len(keys) < 1 || len(keys) > sourcedata.MaxSelectedDatasets || len(queries) < 1 || len(queries) > 10 {
		return "", nil, sourcedata.ErrInput
	}
	args := []any{}
	datasets, names := []string{}, []string{}
	for _, key := range keys {
		base := len(args) + 1
		datasets = append(datasets, fmt.Sprintf("($%d::uuid,$%d::text,$%d::uuid)", base, base+1, base+2))
		args = append(args, key.Owner, key.Dataset, key.Generation)
	}
	for _, query := range queries {
		base := len(args) + 1
		names = append(names, fmt.Sprintf("($%d::text,$%d::text,$%d::jsonb)", base, base+1, base+2))
		raw, e := json.Marshal(query)
		if e != nil {
			return "", nil, e
		}
		args = append(args, sourcedata.Normalize(query.Name), query.Type, raw)
	}
	args = append(args, CandidateBound+1)
	statement := strings.NewReplacer("jsonb_to_recordset($1::jsonb) AS d(owner uuid,dataset text,generation uuid)", "__DATASETS__", "jsonb_to_recordset($2::jsonb) AS j(name text,type text,query jsonb)", "__QUERIES__", "$3", "__BOUND__").Replace(CandidateQuerySQL)
	statement = strings.NewReplacer("__DATASETS__", "(VALUES "+strings.Join(datasets, ",")+") AS d(owner,dataset,generation)", "__QUERIES__", "(VALUES "+strings.Join(names, ",")+") AS j(name,type,query)", "__BOUND__", fmt.Sprintf("$%d", len(args))).Replace(statement)
	return statement, args, nil
}

type RetrievedCandidate struct {
	EntityID string
	Record   sourcedata.Record
	Ref      sourcedata.CandidateRef
	Evidence matching.Evidence
}
type Retrieval struct {
	Candidates []RetrievedCandidate
	Datasets   []Dataset
	Partial    bool
}

// RetrieveCandidates is an internal Go diagnostic surface for the offline
// retrieval/scoring oracle. It is not an HTTP endpoint or an unscoped bypass.
func (p Provider) RetrieveCandidates(ctx context.Context, s provideraccess.Scope, r screening.SearchRequest) (Retrieval, error) {
	v, selected, err := p.prepare(ctx, s, r)
	if err != nil {
		return Retrieval{Candidates: []RetrievedCandidate{}}, err
	}
	return p.retrievePrepared(ctx, r, v, selected)
}
func (p Provider) retrievePrepared(ctx context.Context, r screening.SearchRequest, v sourcedata.Selection, selected []Dataset) (Retrieval, error) {
	out := Retrieval{Candidates: []RetrievedCandidate{}}
	out.Datasets = selected
	keys := []CandidateSelection{}
	for _, d := range selected {
		keys = append(keys, CandidateSelection{Owner: d.owner, Dataset: d.Ref.ID, Generation: *d.Generation})
	}
	statement, args, err := CandidateStatement(keys, r.Queries)
	if err != nil {
		return out, err
	}
	tx, err := p.DB.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT set_config('pg_trgm.similarity_threshold','0.15',true)`); err != nil {
		return out, err
	}
	best := map[string]RetrievedCandidate{}
	scanned := 0
	type queryStatement struct {
		sql  string
		args []any
	}
	statements := []queryStatement{}
	if v.Profile == sourcedata.IdentifierProfile {
		idSQL, idArgs, e := IdentifierCandidateStatement(keys, r.Queries)
		if e != nil {
			return out, e
		}
		if idSQL != "" {
			statements = append(statements, queryStatement{idSQL, idArgs})
		}
	}
	statements = append(statements, queryStatement{statement, args})
	for _, stmt := range statements {
		rows, e := tx.Query(ctx, stmt.sql, stmt.args...)
		if e != nil {
			return out, sourcedata.ErrNotReady
		}
		for rows.Next() {
			if err = ctx.Err(); err != nil {
				rows.Close()
				return out, err
			}
			var c RetrievedCandidate
			var owner, generation, entity uuid.UUID
			var ds, id, alias, nkey, qkey string
			var record, query []byte
			var trigram float64
			if err = rows.Scan(&owner, &ds, &generation, &id, &entity, &record, &alias, &nkey, &qkey, &query, &trigram); err != nil {
				rows.Close()
				return out, err
			}
			scanned++
			if scanned > CandidateBound {
				out.Partial = true
				break
			}
			var q screening.SearchQuery
			if json.Unmarshal(record, &c.Record) != nil || json.Unmarshal(query, &q) != nil {
				rows.Close()
				return out, sourcedata.ErrEvidence
			}
			scope := "global"
			if owner != uuid.Nil {
				scope = "tenant"
			}
			c.EntityID = entity.String()
			c.Ref = sourcedata.CandidateRef{Dataset: sourcedata.DatasetRef{Scope: scope, ID: ds}, Generation: generation, RecordID: id}
			c.Evidence = matching.Score(q, c.Record, alias, qkey, nkey, trigram)
			if v.Profile == sourcedata.TokenProfile {
				c.Evidence = matching.ScoreV2(q, c.Record, alias, qkey, nkey, trigram)
			}
			if v.Profile == sourcedata.IdentifierProfile {
				c.Evidence = matching.ScoreIdentifiers(q, c.Record, alias, qkey, nkey, trigram)
			}
			old, ok := best[c.EntityID]
			if !ok || c.Evidence.Score > old.Evidence.Score || (c.Evidence.Score == old.Evidence.Score && c.Evidence.MatchedValue < old.Evidence.MatchedValue) {
				best[c.EntityID] = c
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return out, err
		}
		if out.Partial {
			break
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	for _, c := range best {
		out.Candidates = append(out.Candidates, c)
	}
	sort.Slice(out.Candidates, func(i, j int) bool {
		a, b := out.Candidates[i], out.Candidates[j]
		if a.Evidence.Score == b.Evidence.Score {
			return a.EntityID < b.EntityID
		}
		return a.Evidence.Score > b.Evidence.Score
	})
	return out, nil
}
func mustSelection(r screening.SearchRequest) sourcedata.Selection {
	v, _ := sourcedata.DecodeSelection(r.ProviderConfig)
	return v
}
func (p Provider) searchFuzzy(ctx context.Context, s provideraccess.Scope, r screening.SearchRequest, v sourcedata.Selection, selected []Dataset) (screening.ProviderResult, error) {
	retrieved, err := p.retrievePrepared(ctx, r, v, selected)
	if err != nil {
		return screening.ProviderResult{}, err
	}
	limit := 10
	if r.LimitOverride != nil {
		limit = *r.LimitOverride
	}
	matches := []screening.ProviderMatch{}
	qualified := 0
	for _, c := range retrieved.Candidates {
		if c.Evidence.Score < float64(v.Threshold)/100 {
			continue
		}
		qualified++
		if len(matches) >= limit {
			continue
		}
		var payload map[string]any
		json.Unmarshal(candidatePayload(c.Record, c.Ref, c.Evidence.MatchedValue), &payload)
		// Only deliberately projected explanation values enter the existing DTO.
		props := payload["properties"].(map[string]any)
		props["matchingProfile"] = []string{v.Profile}
		props["matchingStrategy"] = []string{c.Evidence.Strategy}
		props["matchedField"] = []string{c.Evidence.Field}
		props["comparisonValues"] = []string{c.Evidence.NormalizedQuery + " ↔ " + c.Evidence.NormalizedCandidate}
		props["nameScore"] = []string{strconv.FormatFloat(c.Evidence.NameScore, 'f', 4, 64)}
		attrs := []string{}
		for k, val := range c.Evidence.Attributes {
			attrs = append(attrs, k+": "+val)
		}
		sort.Strings(attrs)
		props["attributeEvidence"] = attrs
		props["matchReason"] = []string{"Name similarity candidate; identity requires review"}
		if v.Profile == sourcedata.IdentifierProfile {
			props["matchReason"] = []string{"Name and exact identifier evidence; candidate requires identity review"}
		}
		payload["profile"] = v.Profile
		payload["matching"] = c.Evidence
		raw, _ := json.Marshal(payload)
		matches = append(matches, screening.ProviderMatch{EntityID: c.EntityID, Name: c.Record.Name, Score: c.Evidence.Score, Payload: raw, MatchedTexts: []string{c.Evidence.MatchedValue}})
	}
	partial := retrieved.Partial || qualified > limit
	raw, _ := json.Marshal(map[string]any{"contract_version": 2, "provider": sourcedata.Provider, "profile": v.Profile, "datasets": retrieved.Datasets, "partial": partial, "retrieved_candidates": len(retrieved.Candidates), "matches": matches})
	encoded, _ := json.Marshal([]any{r, retrieved.Datasets})
	digest := sha256.Sum256(append([]byte(s.Key()), encoded...))
	return screening.ProviderResult{ProviderReference: hex.EncodeToString(digest[:]), RawResponse: raw, Partial: partial, Matches: matches}, nil
}
