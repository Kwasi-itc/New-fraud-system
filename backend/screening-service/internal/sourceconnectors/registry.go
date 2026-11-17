// Package sourceconnectors fetches and parses explicitly named official
// snapshots. It exposes no customer URL endpoint and never activates a source.
package sourceconnectors

import "errors"

var ErrSource = errors.New("unknown_source")
var ErrDownload = errors.New("source_download_failed")
var ErrSchema = errors.New("source_schema_invalid")
var ErrLimit = errors.New("source_limit_exceeded")

type Spec struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	URL       string `json:"url"`
	Reference string `json:"reference"`
	Parser    string `json:"parser"`
	Root      string `json:"root"`
	Namespace string `json:"namespace"`
	Category  string `json:"category"`
	Coverage  string `json:"coverage"`
	Rights    string `json:"rights"`
	hosts     map[string]bool
}

func Registry() []Spec {
	return []Spec{
		{Key: "ofac-sdn", Label: "OFAC SDN", URL: "https://sanctionslistservice.ofac.treas.gov/api/PublicationPreview/exports/SDN.XML", Reference: "https://ofac.treasury.gov/sanctions-list-service", Parser: "ofac-classic-v1", Root: "sdnList", Namespace: "https://sanctionslistservice.ofac.treas.gov/api/PublicationPreview/exports/XML", Category: "sanctions", Coverage: "SDN only; not consolidated non-SDN lists or ownership-derived restrictions", Rights: "Official public-domain evidence recorded; production purpose/privacy/operating review remains separate", hosts: map[string]bool{"sanctionslistservice.ofac.treas.gov": true, "wc2h-sls-prod-public-published.s3.us-gov-west-1.amazonaws.com": true}},
		{Key: "un-consolidated", Label: "UN Security Council consolidated list", URL: "https://scsanctions.un.org/resources/xml/en/consolidated.xml", Reference: "https://main.un.org/securitycouncil/en/sanctions/un-sc-consolidated-list", Parser: "un-consolidated-v1", Root: "CONSOLIDATED_LIST", Category: "sanctions", Coverage: "UN Security Council consolidated individuals and entities; not a PEP database", Rights: "Commercial reuse/retention approval unresolved; source remains disabled", hosts: map[string]bool{"scsanctions.un.org": true, "unsolprodfiles.blob.core.windows.net": true}},
		{Key: "uksl", Label: "UK Sanctions List", URL: "https://sanctionslist.fcdo.gov.uk/docs/UK-Sanctions-List.xml", Reference: "https://www.gov.uk/government/publications/the-uk-sanctions-list", Parser: "uksl-v1", Root: "Designations", Category: "sanctions", Coverage: "Current UKSL designations and specified ships; historical OFSI IDs retained; not obsolete OFSI feed", Rights: "OGL notice subject to personal-data/third-party exceptions and purpose review", hosts: map[string]bool{"sanctionslist.fcdo.gov.uk": true}},
		{Key: "eu-fsf", Label: "EU consolidated financial sanctions", Reference: "https://webgate.ec.europa.eu/fsd/fsf#!/files", Parser: "eu-fsf-1-1", Root: "export", Namespace: "http://eu.europa.ec/fpi/fsd/export", Category: "sanctions", Coverage: "FSF asset-freeze targets; not all EU restrictive measures", Rights: "Approved access/token and source-specific reuse terms required; disabled technical groundwork", hosts: map[string]bool{"webgate.ec.europa.eu": true}},
	}
}
func Lookup(key string) (Spec, error) {
	for _, s := range Registry() {
		if s.Key == key {
			return s, nil
		}
	}
	return Spec{}, ErrSource
}
