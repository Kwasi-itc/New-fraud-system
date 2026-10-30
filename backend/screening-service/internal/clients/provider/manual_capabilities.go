package provider

// ManualCapabilities describes the fixed manual provider, not its health or
// tenant entitlements. Never include deployment addresses or credentials.
type ManualCapabilities struct {
	ID                 string              `json:"id"`
	Name               string              `json:"name"`
	Adapter            string              `json:"adapter"`
	Configured         bool                `json:"configured"`
	Health             string              `json:"health"`
	EntityTypes        []string            `json:"entity_types"`
	Fields             map[string][]string `json:"fields"`
	DatasetSelection   bool                `json:"dataset_selection"`
	ThresholdMin       int                 `json:"threshold_min"`
	ThresholdMax       int                 `json:"threshold_max"`
	LimitMax           int                 `json:"limit_max"`
	Limitations        []string            `json:"limitations"`
	ThresholdSemantics string              `json:"threshold_semantics"`
	ResultSemantics    string              `json:"result_semantics"`
	CandidateMetadata  []string            `json:"candidate_metadata"`
	LiveEnrichment     bool                `json:"live_enrichment"`
}

func (c HTTPClient) ManualCapabilities() ManualCapabilities {
	direct := c.shouldUseOpenSanctions("opensanctions")
	_, err := c.resolveProvider("opensanctions")
	item := ManualCapabilities{ID: "opensanctions", Name: "OpenSanctions", Adapter: "generic", Configured: direct || err == nil,
		Health: "unknown", EntityTypes: []string{"Person", "Organization"},
		Fields:       map[string][]string{"Person": {"name"}, "Organization": {"name"}},
		ThresholdMin: 0, ThresholdMax: 100, LimitMax: 50,
		ThresholdSemantics: "Forwarded to generic adapter; deployment semantics unverified",
		ResultSemantics:    "Bounded requested limit; stored results are reopened without provider calls",
		CandidateMetadata:  []string{"entity_id", "name", "score", "stored_properties", "datasets_when_returned"},
		Limitations:        []string{"Configuration is not proof of provider or worker availability.", "Generic adapter matching semantics require deployment verification."}}
	if direct {
		item.Adapter = "opensanctions_match"
		item.Fields["Person"] = []string{"name", "birth_date", "nationality", "address"}
		item.Fields["Organization"] = []string{"name", "country", "registration_number", "address"}
		item.DatasetSelection = true
		item.ThresholdSemantics = "Percentage divided by 100 and sent as threshold and cutoff"
		item.ResultSemantics = "Requested provider limit; matches only, deduplicated by entity ID and sorted by descending score; truncation indicated as partial"
		item.Limitations = []string{"Configuration is not proof of provider or worker availability.", "Dataset membership and access are checked by the provider.", "Identity fields affect provider matching; they are not guaranteed exact filters."}
		if c.openSanctions.authMode != "" && c.openSanctions.authMode != "none" && c.openSanctions.apiKey == "" {
			item.Configured = false
		}
	}
	return item
}
