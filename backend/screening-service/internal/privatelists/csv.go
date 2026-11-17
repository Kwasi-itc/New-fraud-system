package privatelists

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"golang.org/x/text/language"
	"io"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
)

const MaxCSVBytes = 5 * 1024 * 1024
const MaxCSVRecords = 10000
const MaxRequestBytes = 2*MaxCSVBytes + 8192

// ParseCSV is a full snapshot, never an implicit append/upsert. Empty snapshots
// require a separate explicit clear/archive command and distinct approval.
func ParseCSV(input string) ([]sourcedata.Record, error) {
	if input == "" || len(input) > MaxCSVBytes || !utf8.ValidString(input) || strings.IndexFunc(input, func(r rune) bool { return unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r' }) >= 0 {
		return nil, ErrInput
	}
	input = strings.TrimPrefix(input, "\uFEFF")
	reader := csv.NewReader(strings.NewReader(input))
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		return nil, ErrInput
	}
	allowed := map[string]bool{"record_id": true, "name": true, "entity_type": true, "aliases": true, "source_reference": true, "birth_date": true, "country": true, "nationality": true, "registration_number": true, "address": true, "identifiers": true}
	columns := map[string]int{}
	for i, name := range header {
		if !allowed[name] {
			return nil, fmt.Errorf("%w: unsupported CSV column", ErrInput)
		}
		if _, duplicate := columns[name]; duplicate {
			return nil, fmt.Errorf("%w: duplicate CSV column", ErrInput)
		}
		columns[name] = i
	}
	for _, key := range []string{"record_id", "name", "entity_type", "source_reference"} {
		if _, ok := columns[key]; !ok {
			return nil, fmt.Errorf("%w: required CSV column missing", ErrInput)
		}
	}
	records := []sourcedata.Record{}
	seen := map[string]bool{}
	row := 1
	for {
		values, e := reader.Read()
		if e == io.EOF {
			break
		}
		row++
		if e != nil || len(values) != len(header) {
			return nil, fmt.Errorf("%w: malformed row %d", ErrInput, row)
		}
		get := func(name string) string {
			if i, ok := columns[name]; ok {
				return strings.TrimSpace(values[i])
			}
			return ""
		}
		record := sourcedata.Record{ID: get("record_id"), Name: get("name"), Type: get("entity_type"), SourceReference: get("source_reference"), Properties: map[string][]string{}}
		if aliases := get("aliases"); aliases != "" {
			if json.Unmarshal([]byte(aliases), &record.Aliases) != nil {
				return nil, fmt.Errorf("%w: aliases must be a JSON string array at row %d", ErrInput, row)
			}
		}
		if identifiers := get("identifiers"); identifiers != "" {
			dec := json.NewDecoder(strings.NewReader(identifiers))
			dec.DisallowUnknownFields()
			if dec.Decode(&record.Identifiers) != nil || dec.Decode(new(any)) != io.EOF {
				return nil, fmt.Errorf("%w: invalid typed identifiers at row %d", ErrInput, row)
			}
		}
		for column, key := range map[string]string{"birth_date": "birthDate", "country": "country", "nationality": "nationality", "registration_number": "registrationNumber", "address": "address"} {
			if value := get(column); value != "" {
				record.Properties[key] = []string{value}
			}
		}
		if record.Validate() != nil || !validPrivateProperties(record) || seen[record.ID] {
			return nil, fmt.Errorf("%w: invalid or duplicate source ID at row %d", ErrInput, row)
		}
		seen[record.ID] = true
		records = append(records, record)
		if len(records) > MaxCSVRecords {
			return nil, ErrInput
		}
	}
	if len(records) == 0 {
		return nil, ErrInput
	}
	return records, nil
}

func validPrivateProperties(record sourcedata.Record) bool {
	for _, key := range []string{"country", "nationality"} {
		for _, value := range record.Properties[key] {
			region, e := language.ParseRegion(value)
			if len(value) != 2 || e != nil || !region.IsCountry() {
				return false
			}
		}
	}
	for _, value := range record.Properties["birthDate"] {
		layout := "2006-01-02"
		if len(value) == 4 {
			layout = "2006"
		}
		date, e := time.Parse(layout, value)
		if e != nil || date.Year() < 1 || date.After(time.Now().UTC()) {
			return false
		}
	}
	if record.Type != "Person" && (len(record.Properties["birthDate"]) > 0 || len(record.Properties["nationality"]) > 0) {
		return false
	}
	for _, id := range record.Identifiers {
		if id.Type == "passport" && record.Type != "Person" {
			return false
		}
		if id.Type == "imo" && record.Type != "Vessel" {
			return false
		}
	}
	return true
}
