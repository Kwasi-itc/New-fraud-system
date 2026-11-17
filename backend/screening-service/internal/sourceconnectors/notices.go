package sourceconnectors

import (
	"net/url"
	"strings"
)

// Notice sources are deliberately separate from automatic snapshot connectors.
// A public webpage is neither a bulk API nor evidence of redistribution rights.
type NoticeSpec struct {
	Key, Label, Reference, Category, Acquisition, Coverage string
	Hosts                                                  []string
	Intake                                                 bool
}

func NoticeRegistry() []NoticeSpec {
	return []NoticeSpec{
		{Key: "reviewed-gh-bog", Label: "Bank of Ghana reviewed notices", Reference: "https://www.bog.gov.gh/notice/", Category: "regulatory_warning", Acquisition: "human-reviewed structured snapshot with retained attachment", Coverage: "Only the supplied referenced notices; not a sanctions list or a complete agency feed", Hosts: []string{"bog.gov.gh", "www.bog.gov.gh"}, Intake: true},
		{Key: "reviewed-gh-sec", Label: "Ghana SEC reviewed notices", Reference: "https://sec.gov.gh/public-notices-press-releases/", Category: "regulatory_warning", Acquisition: "human-reviewed structured snapshot with retained attachment", Coverage: "Only the supplied notices; licensing allegations/status must retain dates and corrections", Hosts: []string{"sec.gov.gh", "www.sec.gov.gh"}, Intake: true},
		{Key: "reviewed-gh-osp", Label: "Ghana OSP reviewed notices", Reference: "https://osp.gov.gh/case_files/archive", Category: "proceeding_notice", Acquisition: "human-reviewed structured snapshot with retained attachment", Coverage: "Wanted/proceeding notices, including corrections and withdrawals; not a finding of guilt or PEP classification", Hosts: []string{"osp.gov.gh", "www.osp.gov.gh"}, Intake: true},
		{Key: "gh-fic-references", Label: "Ghana FIC UN references", Reference: "https://fic.gov.gh/publications-sanctions.html", Category: "sanctions", Acquisition: "reference-only", Coverage: "Use the independent authoritative UN connector; no duplicate UN dataset or invented national feed", Hosts: []string{"fic.gov.gh", "www.fic.gov.gh"}},
		{Key: "gh-orc-contract", Label: "Ghana ORC corporate information", Reference: "https://orc.gov.gh/", Category: "corporate_information", Acquisition: "contract-required", Coverage: "Acquisition, schema, purpose, fees and redistribution agreement required; not a sanctions/PEP feed", Hosts: []string{"orc.gov.gh", "www.orc.gov.gh"}},
	}
}
func NoticeSource(key string) (NoticeSpec, error) {
	for _, s := range NoticeRegistry() {
		if s.Key == key {
			return s, nil
		}
	}
	return NoticeSpec{}, ErrSource
}
func (s NoticeSpec) ValidReference(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil || u.Host == "" || u.RawQuery != "" || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	for _, host := range s.Hosts {
		if strings.EqualFold(u.Hostname(), host) {
			return true
		}
	}
	return false
}
