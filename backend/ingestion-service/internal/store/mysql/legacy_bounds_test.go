package mysql

import (
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/ingestion-service/internal/domain/ingestion"
	"strings"
	"testing"
)

func TestLegacyAggregateEvidenceBounds(t *testing.T) {
	const budget = 1 << 20
	for _, name := range []string{"model_text", "model_entries", "audits", "raw", "aggregate"} {
		t.Run(name, func(t *testing.T) {
			var in LegacyImport
			switch name {
			case "model_text":
				in.HistoricalModel.SchemaName = strings.Repeat("x", budget)
			case "model_entries":
				in.HistoricalModel.ManagedSystemFields = make([]string, budget/32)
			case "audits":
				in.Audits = make([]ingestion.IngestionAudit, budget/64)
			case "raw":
				in.RawJSON = make([]byte, budget)
			case "aggregate":
				in.RawJSON = make([]byte, budget/12)
				in.HistoricalModel.SchemaName = strings.Repeat("x", budget/12)
			}
			if _, _, _, err := translateLegacy(in, budget); !errors.Is(err, ErrLegacyEvidenceLimit) {
				t.Fatal("must reject before semantic processing", err)
			}
		})
	}
	if err := boundLegacyEvidence(LegacyImport{}, budget); err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int{0, -1, 17 << 20} {
		if !errors.Is(boundLegacyEvidence(LegacyImport{}, limit), ErrLegacyEvidenceLimit) {
			t.Fatal(limit)
		}
	}
}
