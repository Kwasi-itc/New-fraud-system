package screening

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
	"github.com/google/uuid"
)

var ErrRecognitionUnavailable = errors.New("Name recognition is not configured.")

var ErrMatchingUnavailable = errors.New("Explicit matching settings require the configured direct OpenSanctions adapter.")

var ErrInvalidQuery = errors.New("invalid screening query")

var ErrRegistrationUnavailable = errors.New("Registration-number mapping is unavailable for the configured provider.")

var ErrNationalityUnavailable = errors.New("Nationality mapping is unavailable for the configured provider.")
var ErrCountryUnavailable = errors.New("Country mapping is unavailable for the configured provider.")

var ErrBirthDateUnavailable = errors.New("Birth-date mapping is unavailable for the configured provider.")

var ErrAddressUnavailable = errors.New("Address mapping is unavailable for the configured provider.")

var ErrAuthoringConflict = errors.New("Screening configuration changed or is unsupported. Refresh before saving.")
var datasetIdentifier = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,99}$`)
var fieldName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)

type AuthoringRule struct {
	CounterpartyIDField     string                  `json:"counterparty_id_field,omitempty"`
	ForcedOutcome           string                  `json:"forced_outcome,omitempty"`
	UseNER                  bool                    `json:"use_ner,omitempty"`
	IgnoreNERClassification bool                    `json:"ner_ignore_classification,omitempty"`
	IgnoreListID            string                  `json:"ignore_list_id,omitempty"`
	MinimumNameLength       int                     `json:"minimum_name_length,omitempty"`
	RemoveDigits            bool                    `json:"remove_digits,omitempty"`
	Condition               *workflow.ConditionTree `json:"condition,omitempty"`
	Threshold               *int                    `json:"threshold,omitempty"`
	ResultLimit             *int                    `json:"result_limit,omitempty"`
	Datasets                []string                `json:"datasets,omitempty"`
	ID                      string                  `json:"id"`
	Name                    string                  `json:"name"`
	Enabled                 bool                    `json:"enabled"`
	EntityType              string                  `json:"entity_type"`
	NameField               string                  `json:"name_field"`
	AdditionalNameFields    []string                `json:"additional_name_fields,omitempty"`
	RegistrationField       string                  `json:"registration_number_field,omitempty"`
	NationalityField        string                  `json:"nationality_field,omitempty"`
	CountryField            string                  `json:"country_field,omitempty"`
	BirthDateField          string                  `json:"birth_date_field,omitempty"`
	AddressField            string                  `json:"address_field,omitempty"`
	Outcomes                []string                `json:"outcomes"`
	InboxID                 string                  `json:"inbox_id"`
	UpdatedAt               *time.Time              `json:"updated_at"`
}

func (r AuthoringRule) Validate() error {
	if r.CounterpartyIDField != "" && !fieldName.MatchString(r.CounterpartyIDField) {
		return fmt.Errorf("Choose a supported counterparty text field.")
	}
	if r.ForcedOutcome != "" && r.ForcedOutcome != "review" && r.ForcedOutcome != "block_and_review" && r.ForcedOutcome != "decline" {
		return fmt.Errorf("Choose review, block and review, or decline for a screening hit.")
	}
	if r.IgnoreNERClassification && !r.UseNER {
		return fmt.Errorf("Name recognition must be enabled to ignore its classification.")
	}
	if r.IgnoreListID != "" {
		if id, err := uuid.Parse(r.IgnoreListID); err != nil || id == uuid.Nil {
			return fmt.Errorf("Choose a valid ignore list.")
		}
	}
	if r.MinimumNameLength < 0 || r.MinimumNameLength > 200 {
		return fmt.Errorf("Minimum name length must be from 0 to 200 characters.")
	}
	if r.Condition != nil {
		if err := r.Condition.Validate(); err != nil {
			return err
		}
	}
	if r.Threshold != nil && (*r.Threshold < 0 || *r.Threshold > 100) {
		return fmt.Errorf("Threshold must be an integer from 0 to 100.")
	}
	if r.ResultLimit != nil && (*r.ResultLimit < 1 || *r.ResultLimit > 50) {
		return fmt.Errorf("Result limit must be an integer from 1 to 50.")
	}
	if len(r.Datasets) > 20 {
		return fmt.Errorf("Choose at most 20 datasets.")
	}
	seenDatasets := map[string]bool{}
	for _, d := range r.Datasets {
		if !datasetIdentifier.MatchString(d) || seenDatasets[d] {
			return fmt.Errorf("Dataset identifiers must be unique, nonblank and at most 100 supported characters.")
		}
		seenDatasets[d] = true
	}

	for _, raw := range []string{r.ID, r.InboxID} {
		if id, err := uuid.Parse(raw); err != nil || id == uuid.Nil {
			return fmt.Errorf("Configuration and inbox identifiers are required.")
		}
	}
	if strings.TrimSpace(r.Name) == "" || len(r.Name) > 200 {
		return fmt.Errorf("A name of at most 200 bytes is required.")
	}
	if r.EntityType != "Person" && r.EntityType != "Organization" {
		return fmt.Errorf("Choose Person or Organization.")
	}
	if !fieldName.MatchString(r.NameField) {
		return fmt.Errorf("Choose a supported name field.")
	}
	if len(r.AdditionalNameFields) > 4 {
		return fmt.Errorf("Choose at most five name fields.")
	}
	seenNames := map[string]bool{r.NameField: true}
	for _, field := range r.AdditionalNameFields {
		if !fieldName.MatchString(field) || seenNames[field] {
			return fmt.Errorf("Name fields must be supported and unique.")
		}
		seenNames[field] = true
	}
	if r.AddressField != "" && !fieldName.MatchString(r.AddressField) {
		return fmt.Errorf("Choose a supported address field.")
	}
	if r.BirthDateField != "" && (r.EntityType != "Person" || !fieldName.MatchString(r.BirthDateField)) {
		return fmt.Errorf("Choose a supported birth-date text field for Person only.")
	}
	if r.NationalityField != "" && (r.EntityType != "Person" || !fieldName.MatchString(r.NationalityField)) {
		return fmt.Errorf("Choose a supported nationality text field for Person only.")
	}
	if r.CountryField != "" && (r.EntityType != "Organization" || !fieldName.MatchString(r.CountryField)) {
		return fmt.Errorf("Choose a supported country text field for Organization only.")
	}
	if r.RegistrationField != "" && (r.EntityType != "Organization" || !fieldName.MatchString(r.RegistrationField)) {
		return fmt.Errorf("Choose a supported registration-number text field for Organization only.")
	}
	if (len(r.Outcomes) == 0 && r.ForcedOutcome == "") || len(r.Outcomes) > 4 {
		return fmt.Errorf("Choose at least one decision outcome.")
	}
	seen := map[string]bool{}
	for _, outcome := range r.Outcomes {
		if seen[outcome] {
			return fmt.Errorf("Duplicate decision outcome.")
		}
		seen[outcome] = true
		switch outcome {
		case "approve", "review", "block_and_review", "decline":
		default:
			return fmt.Errorf("Unsupported decision outcome.")
		}
	}
	return nil
}

// Exact canonical ownership marker: legacy/provider-specific configs stay read-only.
func (r AuthoringRule) ConfigJSON() json.RawMessage {
	fields := map[string]string{"name": r.NameField}
	if r.AddressField != "" {
		fields["address"] = r.AddressField
	}
	if r.BirthDateField != "" {
		fields["birth_date"] = r.BirthDateField
	}
	if r.NationalityField != "" {
		fields["nationality"] = r.NationalityField
	}
	if r.CountryField != "" {
		fields["country"] = r.CountryField
	}
	if r.RegistrationField != "" {
		fields["registration_number"] = r.RegistrationField
	}
	config := map[string]any{"authoring_version": 1, "entity_type": r.EntityType, "query_fields": fields}
	if r.CounterpartyIDField != "" {
		config["counterparty_id_field"] = r.CounterpartyIDField
	}
	if r.ForcedOutcome != "" {
		config["forced_outcome"] = r.ForcedOutcome
	}
	if len(r.AdditionalNameFields) > 0 {
		config["additional_name_fields"] = r.AdditionalNameFields
	}
	if r.Condition != nil {
		config["condition"] = r.Condition
	}
	if r.MinimumNameLength != 0 {
		config["minimum_name_length"] = r.MinimumNameLength
	}
	if r.RemoveDigits {
		config["remove_digits"] = true
	}
	if r.IgnoreListID != "" {
		config["ignore_list_id"] = r.IgnoreListID
	}
	if r.UseNER {
		config["use_ner"] = true
	}
	if r.IgnoreNERClassification {
		config["ner_ignore_classification"] = true
	}
	provider := map[string]any{}
	if r.UseNER {
		provider["requires_direct_adapter"] = true
	}
	if r.Threshold != nil {
		provider["threshold"] = *r.Threshold
	}
	if len(r.Datasets) > 0 {
		provider["datasets"] = r.Datasets
	}
	if len(provider) > 0 {
		config["provider_config"] = provider
	}
	if r.ResultLimit != nil {
		config["limit_override"] = *r.ResultLimit
	}
	raw, _ := json.Marshal(config)
	return raw
}

func DecodeAuthoring(c Config) (AuthoringRule, bool) {
	r := AuthoringRule{ID: c.ID, Name: c.Name, Enabled: c.Active, Outcomes: c.AllowedOutcomes, UpdatedAt: &c.UpdatedAt}
	var v struct {
		CounterpartyIDField     string                  `json:"counterparty_id_field"`
		ForcedOutcome           string                  `json:"forced_outcome"`
		AdditionalNameFields    []string                `json:"additional_name_fields"`
		UseNER                  bool                    `json:"use_ner"`
		IgnoreNERClassification bool                    `json:"ner_ignore_classification"`
		IgnoreListID            string                  `json:"ignore_list_id"`
		MinimumNameLength       int                     `json:"minimum_name_length"`
		RemoveDigits            bool                    `json:"remove_digits"`
		Condition               *workflow.ConditionTree `json:"condition"`
		Provider                struct {
			Threshold *int     `json:"threshold"`
			Datasets  []string `json:"datasets"`
		} `json:"provider_config"`
		Limit      *int              `json:"limit_override"`
		Version    int               `json:"authoring_version"`
		EntityType string            `json:"entity_type"`
		Fields     map[string]string `json:"query_fields"`
	}
	if c.Provider != "opensanctions" || c.ReviewInboxID == nil || json.Unmarshal(c.ConfigJSON, &v) != nil || v.Version != 1 {
		return r, false
	}
	r.MinimumNameLength, r.RemoveDigits = v.MinimumNameLength, v.RemoveDigits
	r.UseNER, r.IgnoreNERClassification = v.UseNER, v.IgnoreNERClassification
	r.IgnoreListID = v.IgnoreListID
	r.Condition = v.Condition
	r.Threshold, r.ResultLimit, r.Datasets = v.Provider.Threshold, v.Limit, v.Provider.Datasets
	r.EntityType = v.EntityType
	r.NameField = v.Fields["name"]
	r.AdditionalNameFields = v.AdditionalNameFields
	r.ForcedOutcome = v.ForcedOutcome
	r.CounterpartyIDField = v.CounterpartyIDField
	r.AddressField = v.Fields["address"]
	r.BirthDateField = v.Fields["birth_date"]
	r.NationalityField = v.Fields["nationality"]
	r.CountryField = v.Fields["country"]
	r.RegistrationField = v.Fields["registration_number"]
	r.InboxID = *c.ReviewInboxID
	var actual, expected any
	_ = json.Unmarshal(c.ConfigJSON, &actual)
	_ = json.Unmarshal(r.ConfigJSON(), &expected)
	a, _ := json.Marshal(actual)
	b, _ := json.Marshal(expected)
	return r, r.Validate() == nil && string(a) == string(b)
}

type AuthoringCapabilities struct {
	CounterpartySupported     bool   `json:"counterparty_supported"`
	InlineSupported           bool   `json:"inline_supported"`
	ProviderName              string `json:"provider_name,omitempty"`
	DefaultThreshold          *int   `json:"default_threshold,omitempty"`
	DefaultResultLimit        *int   `json:"default_result_limit,omitempty"`
	ThresholdSupported        bool   `json:"threshold_supported"`
	ResultLimitSupported      bool   `json:"result_limit_supported"`
	DatasetSelectionSupported bool   `json:"dataset_selection_supported"`
	MatchingSupported         bool   `json:"matching_settings_supported"`
	RegistrationSupported     bool   `json:"registration_number_supported"`
	NationalitySupported      bool   `json:"nationality_supported"`
	CountrySupported          bool   `json:"country_supported"`
	AddressSupported          bool   `json:"address_supported"`
	BirthDateSupported        bool   `json:"birth_date_supported"`
}

type IgnoreListOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type AuthoringPage struct {
	NameRecognitionSupported bool               `json:"name_recognition_supported"`
	IgnoreLists              []IgnoreListOption `json:"ignore_lists"`
	AuthoringCapabilities
	ScenarioName string          `json:"scenario_name"`
	Rules        []AuthoringRule `json:"rules"`
	Fields       []string        `json:"fields"`
	OtherRules   int             `json:"other_rules"`
}
