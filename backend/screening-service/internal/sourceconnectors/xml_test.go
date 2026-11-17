package sourceconnectors

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
)

func TestOfficialSourceFixturesPreserveTypesIdentifiersAndProvenance(t *testing.T) {
	expected := map[string]int{"ofac-sdn": 4, "un-consolidated": 2, "uksl": 3, "eu-fsf": 2}
	for _, spec := range Registry() {
		t.Run(spec.Key, func(t *testing.T) {
			input, err := os.ReadFile(filepath.Join("../../testdata/source-fixtures", spec.Key+".xml"))
			if err != nil {
				t.Fatal(err)
			}
			records := []sourcedata.Record{}
			parsed, err := Parse(context.Background(), spec, bytes.NewReader(input), func(r sourcedata.Record) error { records = append(records, r); return nil })
			if err != nil || parsed.Total != expected[spec.Key] {
				t.Fatal(parsed, err)
			}
			first := records[0]
			if first.Name != "Ama Fixture Mensori" || first.Type != "Person" || len(first.Identifiers) != 1 || first.Identifiers[0].Value != "0012345" || first.SourceReference == "" || first.OriginalXML == "" {
				t.Fatal(first)
			}
			if spec.Key == "un-consolidated" {
				if len(first.Properties["birthDate"]) != 0 || len(first.Properties["birthDateText"]) != 1 || first.AliasQualities["Ama M"] != "weak" {
					t.Fatal("uncertain source data lost/coerced", first)
				}
			}
			if spec.Key == "ofac-sdn" && (parsed.ByType["Vessel"] != 1 || parsed.ByType["Aircraft"] != 1) {
				t.Fatal("unsupported types silently dropped/coerced", parsed)
			}
		})
	}
}

func TestOfficialXMLRejectsXXETruncationDuplicateAndWrongSchema(t *testing.T) {
	spec, _ := Lookup("ofac-sdn")
	good, err := os.ReadFile("../../testdata/source-fixtures/ofac-sdn.xml")
	if err != nil {
		t.Fatal(err)
	}
	invalid := map[string]string{
		"xxe":               `<!DOCTYPE sdnList [<!ENTITY xxe SYSTEM "file:///etc/passwd">]>` + string(good),
		"entity":            strings.Replace(string(good), "Ama", "&unknown;", 1),
		"truncated":         string(good[:len(good)/2]),
		"count":             strings.Replace(string(good), "<Record_Count>4", "<Record_Count>5", 1),
		"duplicate":         strings.Replace(string(good), "<uid>200</uid>", "<uid>100</uid>", 1),
		"namespace":         strings.ReplaceAll(string(good), spec.Namespace, "https://untrusted.invalid/schema"),
		"trailing_document": string(good) + string(good),
		"missing_id":        strings.Replace(string(good), "<uid>100</uid>", "", 1),
		"unknown_type":      strings.Replace(string(good), "<sdnType>Aircraft</sdnType>", "<sdnType>NewObjectType</sdnType>", 1),
	}
	for name, input := range invalid {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(context.Background(), spec, bytes.NewReader([]byte(input)), func(sourcedata.Record) error { return nil }); err == nil {
				t.Fatal("unsafe/incomplete XML accepted")
			}
		})
	}
}

// Separate explicit live-artifact run. These ignored files are actual bounded
// retrievals; this test does not claim that fixture success proves live parsing.
func TestOfficialLiveArtifacts(t *testing.T) {
	dir := os.Getenv("SCREENING_SOURCE_ARTIFACT_DIR")
	if dir == "" {
		t.Skip("explicit isolated live-artifact verification only")
	}
	for _, pair := range [][2]string{{"ofac-sdn", "ofac-sdn.xml"}, {"uksl", "uksl.xml"}, {"un-consolidated", "un.xml"}} {
		t.Run(pair[0], func(t *testing.T) {
			spec, _ := Lookup(pair[0])
			file, err := os.Open(filepath.Join(dir, pair[1]))
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			parsed, err := Parse(context.Background(), spec, file, func(sourcedata.Record) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("live artifact: source=%s records=%d by_type=%v publisher_date=%s parser=%s", spec.Key, parsed.Total, parsed.ByType, parsed.PublisherDate, parsed.Parser)
		})
	}
}
