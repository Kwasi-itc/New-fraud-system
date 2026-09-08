package service

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidatePortableDocumentRejectsInvalidDistributionMetadataBeforeImport(t *testing.T) {
	document := PortableDataModelDocument{Version: portableDataModelVersion, Tables: []PortableTable{{
		Name: "transactions",
		Fields: []PortableField{{
			Name: "merchant_id", DataType: "string", DistributionCategory: "sometimes_distributed",
		}},
	}}}
	if err := validatePortableDocument(document); err == nil || !strings.Contains(err.Error(), "distribution category") {
		t.Fatalf("validatePortableDocument() error = %v, want invalid distribution category", err)
	}

	document.Tables[0].Fields[0].DistributionCategory = "few_value_dominated"
	document.Tables[0].Fields[0].ClassificationEvidence = json.RawMessage(`{"broken"`)
	if err := validatePortableDocument(document); err == nil || !strings.Contains(err.Error(), "valid JSON") {
		t.Fatalf("validatePortableDocument() error = %v, want invalid evidence", err)
	}
}
