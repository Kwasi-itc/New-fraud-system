package sourcedata

import (
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"
)

const Provider = "internal-watchlists"
const Profile = "exact-name-v1"
const FuzzyProfile = "name-fuzzy-v1"
const TokenProfile = "name-token-v2"
const IdentifierProfile = "identifier-evidence-v1"
const Normalizer = "nfkc-casefold-u17-v1"
const NormalizerUnicodeVersion = "17.0.0"

func NormalizerSupported() bool { return norm.Version == NormalizerUnicodeVersion }

func IsFuzzyProfile(profile string) bool {
	return profile == FuzzyProfile || profile == TokenProfile || profile == IdentifierProfile
}

var ErrInput = errors.New("invalid_provider_input")
var ErrDataset = errors.New("dataset_unavailable")
var ErrNotReady = errors.New("dataset_not_ready")
var ErrConflict = errors.New("publication_conflict")
var ErrEvidence = errors.New("evidence_unavailable")
var ErrCursor = errors.New("invalid_provider_cursor")
var ErrPublish = errors.New("source_publication_forbidden")
var keyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,79}$`)

type DatasetRef struct {
	Scope string `json:"scope"`
	ID    string `json:"id"`
}

func (r DatasetRef) Valid() bool {
	return (r.Scope == "global" || r.Scope == "tenant") && keyPattern.MatchString(r.ID)
}

type Selection struct {
	Version   int          `json:"version"`
	Profile   string       `json:"profile"`
	Datasets  []DatasetRef `json:"dataset_selection"`
	Threshold int          `json:"threshold"`
}

func DecodeSelection(raw json.RawMessage) (Selection, error) {
	var v Selection
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if d.Decode(&v) != nil || v.Version != 2 || (v.Profile != Profile && !IsFuzzyProfile(v.Profile)) || len(v.Datasets) < 1 || len(v.Datasets) > 10 || v.Threshold < 0 || v.Threshold > 100 || (IsFuzzyProfile(v.Profile) && v.Threshold < 50) {
		return v, ErrInput
	}
	var trailing any
	if !errors.Is(d.Decode(&trailing), io.EOF) {
		return v, ErrInput
	}
	seen := map[DatasetRef]bool{}
	for _, ref := range v.Datasets {
		if !ref.Valid() || seen[ref] {
			return v, ErrInput
		}
		seen[ref] = true
	}
	return v, nil
}

type Record struct {
	ID              string              `json:"id"`
	Name            string              `json:"name"`
	Type            string              `json:"type"`
	Aliases         []string            `json:"aliases,omitempty"`
	Properties      map[string][]string `json:"properties,omitempty"`
	SourceReference string              `json:"source_reference"`
	SourceType      string              `json:"source_type,omitempty"`
	Identifiers     []Identifier        `json:"identifiers,omitempty"`
	AliasQualities  map[string]string   `json:"alias_qualities,omitempty"`
	OriginalXML     string              `json:"original_xml,omitempty"`
}

func Normalize(s string) string {
	s = cases.Fold().String(norm.NFKC.String(s))
	return strings.Join(strings.Fields(s), " ")
}
func (r Record) Validate() error {
	for _, value := range []string{r.ID, r.Name, r.SourceReference, r.SourceType, r.OriginalXML} {
		if !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
			return ErrInput
		}
	}
	if strings.TrimSpace(r.ID) == "" || len(r.ID) > 200 || strings.TrimSpace(r.Name) == "" || !utf8.ValidString(r.Name) || utf8.RuneCountInString(r.Name) > 1000 || (r.Type != "Person" && r.Type != "Organization" && r.Type != "Vessel" && r.Type != "Aircraft") || len(r.Aliases) > 256 || r.SourceReference == "" || len(r.SourceReference) > 500 || len(r.OriginalXML) > 2*1024*1024 || len(r.SourceType) > 100 || len(r.Identifiers) > 256 || len(r.AliasQualities) > 256 {
		return ErrInput
	}
	for _, name := range r.Aliases {
		if Normalize(name) == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 1000 {
			return ErrInput
		}
	}
	for key, values := range r.Properties {
		if key != "country" && key != "nationality" && key != "birthDate" && key != "address" && key != "registrationNumber" && key != "programs" && key != "listedAt" && key != "sanctionsImposed" && key != "sourceStatus" && key != "birthDateText" && key != "countryText" && key != "nationalityText" {
			return ErrInput
		}
		if len(values) > 256 {
			return ErrInput
		}
		for _, v := range values {
			if len(v) > 16000 || !utf8.ValidString(v) {
				return ErrInput
			}
		}
	}
	for _, id := range r.Identifiers {
		if id.Validate() != nil {
			return ErrInput
		}
	}
	for name, quality := range r.AliasQualities {
		if len(name) > 4000 || (quality != "strong" && quality != "weak" && quality != "unspecified") {
			return ErrInput
		}
	}
	return nil
}

// Controlled-v1 bounds/meaning stay unchanged when source-v2 records are added.
func (r Record) ValidateControlled() error {
	if r.Validate() != nil || utf8.RuneCountInString(r.Name) > 200 || (r.Type != "Person" && r.Type != "Organization") || len(r.Aliases) > 20 || len(r.Identifiers) > 0 || len(r.AliasQualities) > 0 || r.OriginalXML != "" || r.SourceType != "" {
		return ErrInput
	}
	for _, name := range r.Aliases {
		if utf8.RuneCountInString(name) > 200 {
			return ErrInput
		}
	}
	for key, values := range r.Properties {
		if key != "country" && key != "nationality" && key != "birthDate" && key != "address" && key != "registrationNumber" {
			return ErrInput
		}
		if len(values) > 20 {
			return ErrInput
		}
		for _, v := range values {
			if len(v) > 500 {
				return ErrInput
			}
		}
	}
	return nil
}

type CandidateRef struct {
	Dataset    DatasetRef `json:"dataset"`
	Generation uuid.UUID  `json:"generation"`
	RecordID   string     `json:"record_id"`
}
type Operation struct {
	Provider  string
	Selection []DatasetRef
	Cursor    string
	Limit     int
}
