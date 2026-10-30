package mysql

import (
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
)

// This DTO consumes the owner's JSON contract, not the decision caller's model.
// Names come from that versioned publication and are validated, never guessed
// into SQL. The owner must have attested actual generated expression/index shape.
type registryTextEqualityIndex struct {
	FieldID    uuid.UUID `json:"field_id"`
	IndexID    uuid.UUID `json:"index_id"`
	Strategy   string    `json:"strategy"`
	HashColumn string    `json:"hash_column"`
	IndexName  string    `json:"index_name"`
}

func (t *registryTable) bindTextEqualityIndexes(indexes []registryTextEqualityIndex) error {
	if len(indexes) > 64 {
		return ErrPublishedModelUnavailable
	}
	seen := map[uuid.UUID]bool{}
	byID := map[uuid.UUID]string{}
	for name, f := range t.fields {
		byID[f.ID] = name
	}
	for _, m := range indexes {
		name, ok := byID[m.FieldID]
		f := t.fields[name]
		suffix := strings.ReplaceAll(m.IndexID.String(), "-", "")
		if !ok || f.Archived || f.Type != "string" || name == t.lookup || m.IndexID == uuid.Nil || seen[m.IndexID] || f.textEqualityHash != "" || m.Strategy != "sha256_binary_v1" || m.HashColumn != "record_text_hash_"+suffix || m.IndexName != "record_text_index_"+suffix {
			return ErrPublishedModelUnavailable
		}
		column, err := mysqlstore.Identifier(m.HashColumn)
		if err != nil {
			return ErrPublishedModelUnavailable
		}
		f.textEqualityHash = column
		t.fields[name] = f
		seen[m.IndexID] = true
	}
	return nil
}
func (f registryField) textEqualityPredicate(value mysqlstore.Text) (string, []any) {
	// Never use digest-only equality: this residual is the authority even if
	// multiple different complete values share one bucket. Parameters are bound
	// twice, not interpolated, and NULL is rejected by the scalar encoder first.
	return "(" + f.textEqualityHash + "=UNHEX(SHA2(CAST(? AS BINARY),256)) AND CAST(" + f.columns["value"] + " AS BINARY)=CAST(? AS BINARY))", []any{value, value}
}
