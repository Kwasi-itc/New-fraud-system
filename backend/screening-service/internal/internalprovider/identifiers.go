package internalprovider

import (
	"encoding/json"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"strings"
)

// Indexed exact lookup is independent of name retrieval. Values bind as text;
// there is no case folding, fuzzy comparison or numeric identifier conversion.
func IdentifierCandidateStatement(keys []CandidateSelection, queries []screening.SearchQuery) (string, []any, error) {
	if len(keys) < 1 || len(keys) > 10 || len(queries) < 1 || len(queries) > 10 {
		return "", nil, sourcedata.ErrInput
	}
	args := []any{}
	datasets, ids := []string{}, []string{}
	for _, key := range keys {
		n := len(args) + 1
		datasets = append(datasets, fmt.Sprintf("($%d::uuid,$%d::text,$%d::uuid)", n, n+1, n+2))
		args = append(args, key.Owner, key.Dataset, key.Generation)
	}
	for _, q := range queries {
		if sourcedata.ValidateQueryIdentifiers(q.Identifiers) != nil {
			return "", nil, sourcedata.ErrInput
		}
		raw, _ := json.Marshal(q)
		for _, id := range q.Identifiers {
			n := len(args) + 1
			ids = append(ids, fmt.Sprintf("($%d::text,$%d::text,$%d::text,$%d::text,$%d::text,$%d::jsonb,$%d::text)", n, n+1, n+2, n+3, n+4, n+5, n+6))
			args = append(args, id.Type, sourcedata.NormalizeIdentifier(id.Value), strings.ToLower(id.Country), strings.TrimSpace(id.Issuer), sourcedata.Normalize(q.Name), raw, q.Type)
		}
	}
	if len(ids) == 0 {
		return "", nil, nil
	}
	args = append(args, CandidateBound+1)
	sql := `SELECT r.owner_scope,r.dataset_id,r.generation,r.record_id,r.entity_id,r.record-'original_xml',r.record->>'name',screening.name_key_v1(r.record->>'name'),screening.name_key_v1(q.name),q.query,0::float8
 FROM (VALUES ` + strings.Join(datasets, ",") + `) AS d(owner,dataset,generation)
 CROSS JOIN (VALUES ` + strings.Join(ids, ",") + `) AS q(type,value,country,issuer,name,query,entity_type)
 JOIN screening.source_identifiers i ON (i.owner_scope,i.dataset_id,i.generation,i.type,i.normalized)=(d.owner,d.dataset,d.generation,q.type,q.value) AND lower(i.country)=q.country AND btrim(i.issuer)=q.issuer
 JOIN screening.source_records r ON(r.owner_scope,r.dataset_id,r.generation,r.record_id)=(i.owner_scope,i.dataset_id,i.generation,i.record_id) AND r.entity_type=q.entity_type
 ORDER BY r.entity_id,q.type,q.value LIMIT $` + fmt.Sprint(len(args))
	return sql, args, nil
}
