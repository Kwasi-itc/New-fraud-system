package sourceconnectors

import "testing"

func TestNoticeClassificationAndReferences(t *testing.T) {
	sec, e := NoticeSource("reviewed-gh-sec")
	if e != nil || sec.Category != "regulatory_warning" || !sec.Intake {
		t.Fatal(sec, e)
	}
	osp, _ := NoticeSource("reviewed-gh-osp")
	if osp.Category != "proceeding_notice" {
		t.Fatal("allegation classified as sanctions")
	}
	for _, href := range []string{"http://sec.gov.gh/notice", "https://sec.gov.gh.evil.invalid/notice", "https://user:pass@sec.gov.gh/notice", "https://127.0.0.1/notice", "https://sec.gov.gh:444/notice", "https://sec.gov.gh/notice?token=secret"} {
		if sec.ValidReference(href) {
			t.Fatal("unsafe/noncanonical reference", href)
		}
	}
	for _, key := range []string{"gh-fic-references", "gh-orc-contract"} {
		spec, _ := NoticeSource(key)
		if spec.Intake {
			t.Fatal("unapproved acquisition enabled", key)
		}
	}
}
