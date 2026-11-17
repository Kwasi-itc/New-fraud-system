package sourceingest

import (
	"context"
	"github.com/google/uuid"
)

type Change struct {
	RecordID string   `json:"record_id"`
	Kind     string   `json:"kind"`
	Fields   []string `json:"fields"`
}
type DiffPage struct {
	Changes []Change `json:"changes"`
	Next    string   `json:"next,omitempty"`
	HasMore bool     `json:"has_more"`
}

// Diff preserves source identities and original field-level changes, including
// alias/property removal. It never links records across datasets by name.
func (s Store) Diff(ctx context.Context, key Key, from, to uuid.UUID, after string, limit int) (DiffPage, error) {
	if err := s.checkKey(key); err != nil {
		return DiffPage{}, err
	}
	if _, err := s.publisher(ctx); err != nil {
		return DiffPage{}, err
	}
	if limit < 1 || limit > 200 {
		limit = 100
	}
	rows, err := s.DB.Query(ctx, `WITH old AS(SELECT record_id,record FROM screening.source_records WHERE owner_scope=$1 AND dataset_id=$2 AND generation=$3),
 new AS(SELECT record_id,record FROM screening.source_records WHERE owner_scope=$1 AND dataset_id=$2 AND generation=$4)
 SELECT coalesce(old.record_id,new.record_id),CASE WHEN old.record_id IS NULL THEN 'added' WHEN new.record_id IS NULL THEN 'removed' ELSE 'modified' END,
 ARRAY(SELECT field FROM(SELECT jsonb_object_keys(coalesce(old.record,'{}')) field UNION SELECT jsonb_object_keys(coalesce(new.record,'{}')) field) keys WHERE old.record->field IS DISTINCT FROM new.record->field ORDER BY field)
 FROM old FULL OUTER JOIN new USING(record_id) WHERE old.record IS DISTINCT FROM new.record AND coalesce(old.record_id,new.record_id)>$5
 ORDER BY coalesce(old.record_id,new.record_id) LIMIT $6`, key.Owner, key.Dataset, from, to, after, limit+1)
	if err != nil {
		return DiffPage{}, err
	}
	defer rows.Close()
	out := DiffPage{Changes: []Change{}}
	for rows.Next() {
		var c Change
		if err = rows.Scan(&c.RecordID, &c.Kind, &c.Fields); err != nil {
			return DiffPage{}, err
		}
		out.Changes = append(out.Changes, c)
	}
	if err = rows.Err(); err != nil {
		return DiffPage{}, err
	}
	if len(out.Changes) > limit {
		out.HasMore = true
		out.Changes = out.Changes[:limit]
		out.Next = out.Changes[len(out.Changes)-1].RecordID
	}
	return out, nil
}
