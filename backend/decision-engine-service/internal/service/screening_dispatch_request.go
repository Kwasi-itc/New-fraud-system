package service

import (
	"context"
	"encoding/json"
	"fmt"
	"golang.org/x/text/language"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
)

type screeningDispatchConfig struct {
	UseNER                       bool                     `json:"use_ner"`
	IgnoreNERClassification      bool                     `json:"ner_ignore_classification"`
	IgnoreListID                 string                   `json:"ignore_list_id"`
	AuthoringVersion             int                      `json:"authoring_version"`
	MinimumNameLength            int                      `json:"minimum_name_length"`
	RemoveDigits                 bool                     `json:"remove_digits"`
	EntityType                   string                   `json:"entity_type"`
	Queries                      []screeningDispatchQuery `json:"queries"`
	Query                        map[string]any           `json:"query"`
	QueryFields                  map[string]string        `json:"query_fields"`
	AdditionalNameFields         []string                 `json:"additional_name_fields"`
	ProviderConfig               json.RawMessage          `json:"provider_config"`
	LimitOverride                *int                     `json:"limit_override"`
	UniqueCounterpartyIdentifier *string                  `json:"unique_counterparty_identifier"`
	CounterpartyIDField          string                   `json:"counterparty_id_field"`
}

type screeningDispatchQuery struct {
	RegistrationNumber string `json:"registration_number,omitempty"`
	Nationality        string `json:"nationality,omitempty"`
	Country            string `json:"country,omitempty"`
	BirthDate          string `json:"birth_date,omitempty"`
	Address            string `json:"address,omitempty"`
	Name               string `json:"name"`
	Type               string `json:"type,omitempty"`
}

type screeningDispatchRequest struct {
	Provider                     string                   `json:"provider"`
	DecisionID                   string                   `json:"decision_id"`
	ScenarioID                   string                   `json:"scenario_id"`
	ScreeningConfigID            string                   `json:"screening_config_id"`
	IdempotencyKey               string                   `json:"idempotency_key"`
	ObjectType                   string                   `json:"object_type"`
	ObjectID                     string                   `json:"object_id"`
	Queries                      []screeningDispatchQuery `json:"queries"`
	ProviderConfig               json.RawMessage          `json:"provider_config,omitempty"`
	LimitOverride                *int                     `json:"limit_override,omitempty"`
	UniqueCounterpartyIdentifier *string                  `json:"unique_counterparty_identifier,omitempty"`
}

func buildScreeningDispatchRequest(execID string, cfg screening.Config, item screening.Execution, objectType, objectID string, objectFields map[string]any) ([]byte, error) {
	return buildScreeningDispatchWithWords(execID, cfg, item, objectType, objectID, objectFields, nil)
}
func buildScreeningDispatchWithWords(execID string, cfg screening.Config, item screening.Execution, objectType, objectID string, objectFields map[string]any, ignoreWords []string) ([]byte, error) {
	return buildScreeningDispatchPreprocessed(context.Background(), execID, cfg, item, objectType, objectID, objectFields, ignoreWords, nil)
}
func buildScreeningDispatchPreprocessed(ctx context.Context, execID string, cfg screening.Config, item screening.Execution, objectType, objectID string, objectFields map[string]any, ignoreWords []string, recognize screening.NameRecognizer) ([]byte, error) {
	var config screeningDispatchConfig
	if len(cfg.ConfigJSON) > 0 {
		if err := json.Unmarshal(cfg.ConfigJSON, &config); err != nil {
			return nil, fmt.Errorf("parse screening config %s: %w", cfg.ID, err)
		}
	}

	queries := make([]screeningDispatchQuery, 0, len(config.Queries))
	for _, query := range config.Queries {
		name := strings.TrimSpace(query.Name)
		if name == "" {
			continue
		}
		queryType := strings.TrimSpace(query.Type)
		if queryType == "" {
			queryType = strings.TrimSpace(config.EntityType)
		}
		queries = append(queries, screeningDispatchQuery{Name: name, Type: queryType})
	}

	if len(queries) == 0 {
		name, err := resolveScreeningQueryName(config, objectFields)
		if err != nil {
			return nil, err
		}
		if name != "" {
			queries = append(queries, screeningDispatchQuery{
				Name: name,
				Type: strings.TrimSpace(config.EntityType),
			})
		}
	}
	if field := config.QueryFields["address"]; field != "" {
		if len(config.Queries) > 0 {
			return nil, fmt.Errorf("Address mapping cannot be combined with explicit queries.")
		}
		value := objectFields[field]
		if value != nil {
			address, ok := value.(string)
			if !ok {
				return nil, invalidScreeningQuery("Mapped address must be text.")
			}
			address = strings.TrimSpace(address)
			if utf8.RuneCountInString(address) > 300 {
				return nil, invalidScreeningQuery("Mapped address exceeds 300 characters.")
			}
			for i := range queries {
				queries[i].Address = address
			}
		}
	}
	if field := config.QueryFields["birth_date"]; field != "" {
		if config.EntityType != "Person" || len(config.Queries) > 0 {
			return nil, fmt.Errorf("Birth-date mapping requires a mapped Person query.")
		}
		if value := objectFields[field]; value != nil {
			date, ok := value.(string)
			if !ok {
				return nil, invalidScreeningQuery("Mapped birth date must be text.")
			}
			date = strings.TrimSpace(date)
			if date != "" {
				layout := "2006-01-02"
				if len(date) == 4 {
					layout = "2006"
				}
				parsed, err := time.Parse(layout, date)
				if err != nil || parsed.Year() < 1 || parsed.After(time.Now().UTC()) {
					return nil, invalidScreeningQuery("Mapped birth date must be a valid past or current YYYY or YYYY-MM-DD value.")
				}
			}
			for i := range queries {
				queries[i].BirthDate = date
			}
		}
	}

	for _, mapping := range []struct{ key, entity string }{{"nationality", "Person"}, {"country", "Organization"}} {
		field := config.QueryFields[mapping.key]
		if field == "" {
			continue
		}
		if config.EntityType != mapping.entity || len(config.Queries) > 0 {
			return nil, invalidScreeningQuery("Mapped %s requires a mapped %s query.", mapping.key, mapping.entity)
		}
		value := objectFields[field]
		if value == nil {
			continue
		}
		code, ok := value.(string)
		if !ok {
			return nil, invalidScreeningQuery("Mapped %s must be text.", mapping.key)
		}
		code = strings.ToLower(strings.TrimSpace(code))
		if code != "" {
			region, err := language.ParseRegion(code)
			if len(code) != 2 || err != nil || !region.IsCountry() {
				return nil, invalidScreeningQuery("Mapped %s must be a valid two-letter country code.", mapping.key)
			}
		}
		for i := range queries {
			if mapping.key == "nationality" {
				queries[i].Nationality = code
			} else {
				queries[i].Country = code
			}
		}
	}
	if field := config.QueryFields["registration_number"]; field != "" {
		if config.EntityType != "Organization" || len(config.Queries) > 0 {
			return nil, fmt.Errorf("Registration-number mapping requires a mapped Organization query.")
		}
		if value := objectFields[field]; value != nil {
			identifier, ok := value.(string)
			if !ok {
				return nil, invalidScreeningQuery("Mapped registration number must be text.")
			}
			identifier = strings.TrimSpace(identifier)
			if utf8.RuneCountInString(identifier) > 100 {
				return nil, invalidScreeningQuery("Mapped registration number exceeds 100 characters.")
			}
			for i := range queries {
				queries[i].RegistrationNumber = identifier
			}
		}
	}
	if len(queries) == 0 {
		return nil, fmt.Errorf("screening config %s does not define a usable screening query", cfg.ID)
	}

	if config.MinimumNameLength < 0 || config.MinimumNameLength > 200 {
		return nil, invalidScreeningQuery("Invalid minimum name length.")
	}
	if config.UseNER {
		if recognize == nil {
			return nil, invalidScreeningQuery("Name recognition is not configured.")
		}
		original := queries
		seen := map[string]bool{}
		for _, q := range queries {
			seen[q.Name] = true
		}
		for _, q := range original {
			if utf8.RuneCountInString(q.Name) < config.MinimumNameLength {
				continue
			}
			matches, err := recognize(ctx, q.Name)
			if err != nil {
				return nil, screening.ErrRecognitionFailed
			}
			for _, m := range matches {
				entity := m.Type
				if entity == "Company" {
					entity = "Organization"
				}
				if config.IgnoreNERClassification {
					entity = "Thing"
				} else if entity != "Person" && entity != "Organization" {
					continue
				}
				name := strings.TrimSpace(m.Text)
				if name == "" || seen[name] {
					continue
				}
				seen[name] = true
				queries = append(queries, screeningDispatchQuery{Name: name, Type: entity})
			}
		}
	}
	if config.MinimumNameLength > 0 || config.RemoveDigits || config.IgnoreListID != "" {
		ignored := map[string]bool{}
		for _, word := range ignoreWords {
			ignored[strings.ToLower(word)] = true
		}
		filtered := make([]screeningDispatchQuery, 0, len(queries))
		for _, query := range queries {
			if utf8.RuneCountInString(query.Name) < config.MinimumNameLength {
				continue
			}
			if config.RemoveDigits {
				query.Name = strings.TrimSpace(strings.Map(func(r rune) rune {
					if unicode.IsDigit(r) {
						return -1
					}
					return r
				}, query.Name))
			}
			if config.IgnoreListID != "" {
				words := strings.Fields(query.Name)
				kept := words[:0]
				for _, word := range words {
					if !ignored[strings.ToLower(word)] {
						kept = append(kept, word)
					}
				}
				query.Name = strings.Join(kept, " ")
			}
			if query.Name == "" || utf8.RuneCountInString(query.Name) < config.MinimumNameLength {
				continue
			}
			filtered = append(filtered, query)
		}
		queries = filtered
		// An explicit preprocessing skip is not a provider no-hit result.
		if len(queries) == 0 {
			return nil, nil
		}
	}

	uniqueCounterpartyIdentifier := config.UniqueCounterpartyIdentifier
	if uniqueCounterpartyIdentifier == nil && config.CounterpartyIDField != "" {
		if raw := objectFields[config.CounterpartyIDField]; raw != nil {
			value, ok := raw.(string)
			if !ok || len(value) > 512 {
				return nil, invalidScreeningQuery("Counterparty identifier must be text of at most 512 bytes.")
			}
			value = strings.TrimSpace(value)
			if value != "" {
				uniqueCounterpartyIdentifier = &value
			}
		}
	}

	request := screeningDispatchRequest{
		Provider:                     cfg.Provider,
		DecisionID:                   item.DecisionID,
		ScenarioID:                   item.ScenarioID,
		ScreeningConfigID:            cfg.ID,
		IdempotencyKey:               execID,
		ObjectType:                   objectType,
		ObjectID:                     objectID,
		Queries:                      queries,
		ProviderConfig:               config.ProviderConfig,
		LimitOverride:                config.LimitOverride,
		UniqueCounterpartyIdentifier: uniqueCounterpartyIdentifier,
	}
	return json.Marshal(request)
}

func resolveScreeningQueryName(config screeningDispatchConfig, objectFields map[string]any) (string, error) {
	if len(config.AdditionalNameFields) > 0 {
		if config.AuthoringVersion != 1 || len(config.AdditionalNameFields) > 4 || config.QueryFields["name"] == "" {
			return "", invalidScreeningQuery("Invalid combined name mapping.")
		}
		parts := []string{}
		seen := map[string]bool{}
		for _, field := range append([]string{config.QueryFields["name"]}, config.AdditionalNameFields...) {
			if field == "" || seen[field] {
				return "", invalidScreeningQuery("Combined name fields must be nonblank and unique.")
			}
			seen[field] = true
			value := objectFields[field]
			if value == nil {
				continue
			}
			text, ok := value.(string)
			if !ok {
				return "", invalidScreeningQuery("Mapped name fields must be text.")
			}
			if text = strings.TrimSpace(text); text != "" {
				parts = append(parts, text)
			}
		}
		name := strings.Join(parts, " ")
		if name == "" || utf8.RuneCountInString(name) > 200 {
			return "", invalidScreeningQuery("Combined name must contain 1 to 200 characters.")
		}
		return name, nil
	}
	if fieldName := strings.TrimSpace(config.QueryFields["name"]); fieldName != "" {
		if config.AuthoringVersion == 1 && objectFields[fieldName] != nil {
			if _, ok := objectFields[fieldName].(string); !ok {
				return "", invalidScreeningQuery("Mapped name must be text.")
			}
		}
		value := lookupStringField(objectFields, fieldName)
		if value == "" {
			return "", fmt.Errorf("screening query field %q is missing or empty", fieldName)
		}
		return value, nil
	}

	if rawName, ok := config.Query["name"]; ok {
		switch value := rawName.(type) {
		case string:
			trimmed := strings.TrimSpace(value)
			if trimmed == "" {
				return "", nil
			}
			if fieldValue := lookupStringField(objectFields, trimmed); fieldValue != "" {
				return fieldValue, nil
			}
			return trimmed, nil
		default:
			return "", fmt.Errorf("screening query name must be a string")
		}
	}

	return "", nil
}

func lookupStringField(fields map[string]any, fieldName string) string {
	if len(fields) == 0 {
		return ""
	}
	value, ok := fields[fieldName]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

// Classify mapped-value failures without depending on human-readable wording.
func invalidScreeningQuery(format string, args ...any) error {
	return fmt.Errorf("%w: %s", screening.ErrInvalidQuery, fmt.Sprintf(format, args...))
}
