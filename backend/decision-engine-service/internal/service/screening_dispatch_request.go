package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
)

type screeningDispatchConfig struct {
	EntityType                   string                   `json:"entity_type"`
	Queries                      []screeningDispatchQuery `json:"queries"`
	Query                        map[string]any           `json:"query"`
	QueryFields                  map[string]string        `json:"query_fields"`
	ProviderConfig               json.RawMessage          `json:"provider_config"`
	LimitOverride                *int                     `json:"limit_override"`
	UniqueCounterpartyIdentifier *string                  `json:"unique_counterparty_identifier"`
	CounterpartyIDField          string                   `json:"counterparty_id_field"`
}

type screeningDispatchQuery struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
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
	if len(queries) == 0 {
		return nil, fmt.Errorf("screening config %s does not define a usable screening query", cfg.ID)
	}

	uniqueCounterpartyIdentifier := config.UniqueCounterpartyIdentifier
	if uniqueCounterpartyIdentifier == nil && config.CounterpartyIDField != "" {
		if value := lookupStringField(objectFields, config.CounterpartyIDField); value != "" {
			uniqueCounterpartyIdentifier = &value
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
	if fieldName := strings.TrimSpace(config.QueryFields["name"]); fieldName != "" {
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
