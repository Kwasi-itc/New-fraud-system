package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"golang.org/x/text/language"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/ports"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrManualInput = errors.New("invalid manual search")
var ErrManualConflict = errors.New("operation ID already used for a different search")

// ManualSearchInput deliberately excludes tenant, provider credentials, case links,
// and arbitrary provider configuration. Identity comes from the authenticated API.
type ManualSearchInput struct {
	Identifiers        []sourcedata.Identifier `json:"identifiers,omitempty"`
	Provider           string                  `json:"provider,omitempty"`
	Profile            string                  `json:"profile,omitempty"`
	DatasetSelection   []sourcedata.DatasetRef `json:"dataset_selection,omitempty"`
	OperationID        string                  `json:"operation_id"`
	Name               string                  `json:"name"`
	EntityType         string                  `json:"entity_type"`
	Threshold          int                     `json:"threshold"`
	Limit              int                     `json:"limit"`
	BirthDate          string                  `json:"birth_date,omitempty"`
	Nationality        string                  `json:"nationality,omitempty"`
	Country            string                  `json:"country,omitempty"`
	RegistrationNumber string                  `json:"registration_number,omitempty"`
	Address            string                  `json:"address,omitempty"`
	Datasets           []string                `json:"datasets,omitempty"`
}

func (in ManualSearchInput) Request(actor string) (screening.SearchRequest, error) {
	id, err := uuid.Parse(in.OperationID)
	name := strings.TrimSpace(in.Name)
	if err != nil || id == uuid.Nil || actor == "" || name == "" || utf8.RuneCountInString(name) > 200 || (in.EntityType != "Person" && in.EntityType != "Organization" && !(in.Provider == sourcedata.Provider && (in.EntityType == "Vessel" || in.EntityType == "Aircraft"))) || in.Threshold < 0 || in.Threshold > 100 || in.Limit < 1 || in.Limit > 50 {
		return screening.SearchRequest{}, ErrManualInput
	}
	query := screening.SearchQuery{Name: name, Type: in.EntityType, BirthDate: strings.TrimSpace(in.BirthDate), Nationality: strings.ToLower(strings.TrimSpace(in.Nationality)), Country: strings.ToLower(strings.TrimSpace(in.Country)), RegistrationNumber: strings.TrimSpace(in.RegistrationNumber), Address: strings.TrimSpace(in.Address)}
	query.Identifiers = in.Identifiers
	if sourcedata.ValidateQueryIdentifiers(in.Identifiers) != nil || (len(in.Identifiers) > 0 && (in.Provider != sourcedata.Provider || in.Profile != sourcedata.IdentifierProfile)) {
		return screening.SearchRequest{}, ErrManualInput
	}
	if (in.EntityType == "Person" && (query.Country != "" || query.RegistrationNumber != "")) || (in.EntityType == "Organization" && (query.BirthDate != "" || query.Nationality != "")) {
		return screening.SearchRequest{}, ErrManualInput
	}
	for _, code := range []string{query.Country, query.Nationality} {
		if code != "" {
			region, e := language.ParseRegion(code)
			if len(code) != 2 || e != nil || !region.IsCountry() {
				return screening.SearchRequest{}, ErrManualInput
			}
		}
	}
	if utf8.RuneCountInString(query.Address) > 300 || utf8.RuneCountInString(query.RegistrationNumber) > 100 {
		return screening.SearchRequest{}, ErrManualInput
	}
	if query.BirthDate != "" {
		layout := "2006-01-02"
		if len(query.BirthDate) == 4 {
			layout = "2006"
		}
		d, e := time.Parse(layout, query.BirthDate)
		if e != nil || d.Year() < 1 || d.After(time.Now().UTC()) {
			return screening.SearchRequest{}, ErrManualInput
		}
	}
	if len(in.Datasets) > 10 {
		return screening.SearchRequest{}, ErrManualInput
	}
	datasets := []string{}
	seen := map[string]bool{}
	for _, value := range in.Datasets {
		value = strings.TrimSpace(value)
		if !datasetIDPattern.MatchString(value) {
			return screening.SearchRequest{}, ErrManualInput
		}
		if !seen[value] {
			datasets = append(datasets, value)
			seen[value] = true
		}
	}
	config, _ := json.Marshal(struct {
		Threshold int      `json:"threshold"`
		Datasets  []string `json:"datasets,omitempty"`
	}{in.Threshold, datasets})
	provider := in.Provider
	if provider == "" {
		provider = "opensanctions"
	} // documented v1 identity, not registry fallback
	if provider == sourcedata.Provider {
		profile := in.Profile
		if profile == "" {
			profile = sourcedata.Profile
		}
		if len(in.Datasets) > 0 || (profile == sourcedata.Profile && (query.BirthDate != "" || query.Nationality != "" || query.Country != "" || query.RegistrationNumber != "" || query.Address != "")) {
			return screening.SearchRequest{}, ErrManualInput
		}
		config, _ = json.Marshal(sourcedata.Selection{Version: 2, Profile: profile, Datasets: in.DatasetSelection, Threshold: in.Threshold})
		if _, err := sourcedata.DecodeSelection(config); err != nil {
			return screening.SearchRequest{}, ErrManualInput
		}
	} else if provider != "opensanctions" || len(in.DatasetSelection) > 0 || in.Profile != "" {
		return screening.SearchRequest{}, ErrManualInput
	}
	return screening.SearchRequest{
		Provider: provider, ObjectType: in.EntityType, ObjectID: id.String(),
		IdempotencyKey: manualPrefix(actor) + id.String(), IsManual: true,
		Queries:        []screening.SearchQuery{query},
		ProviderConfig: config, LimitOverride: &in.Limit,
	}, nil
}

var datasetIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,79}$`)

// Projection of original settings, not raw provider configuration or a new run.
func ManualSearchSettings(raw json.RawMessage) ManualSearchInput {
	var req screening.SearchRequest
	var in ManualSearchInput
	if json.Unmarshal(raw, &req) != nil || len(req.Queries) == 0 {
		return in
	}
	q := req.Queries[0]
	if req.Provider == sourcedata.Provider {
		in.Provider = req.Provider
		if selected, err := sourcedata.DecodeSelection(req.ProviderConfig); err == nil {
			in.DatasetSelection = selected.Datasets
			in.Profile = selected.Profile
		}
	}
	in.Name = q.Name
	in.EntityType = q.Type
	in.BirthDate = q.BirthDate
	in.Nationality = q.Nationality
	in.Country = q.Country
	in.RegistrationNumber = q.RegistrationNumber
	in.Address = q.Address
	in.Identifiers = q.Identifiers
	var config struct {
		Threshold int      `json:"threshold"`
		Datasets  []string `json:"datasets"`
	}
	_ = json.Unmarshal(req.ProviderConfig, &config)
	in.Threshold = config.Threshold
	in.Datasets = config.Datasets
	if req.LimitOverride != nil {
		in.Limit = *req.LimitOverride
	}
	return in
}

func manualPrefix(actor string) string { return "manual:" + actor + ":" }

func (s ScreeningService) CreateManualSearch(ctx context.Context, tenant, actor string, in ManualSearchInput) (screening.Screening, error) {
	req, err := in.Request(actor)
	if err != nil {
		return screening.Screening{}, err
	}
	if err := s.validateProvider(ctx, tenant, req); err != nil {
		return screening.Screening{}, err
	}
	var result screening.Screening
	err = s.txManager.Run(ctx, func(store ports.MutationStore) error {
		existing, err := store.Screenings().GetByIdempotencyKey(ctx, tenant, req.IdempotencyKey)
		if err == nil {
			if !sameManualRequest(existing.RequestJSON, req) {
				return ErrManualConflict
			}
			result = existing
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		result, err = s.createScreening(ctx, store, tenant, req)
		// The second lookup in createScreening can observe a concurrent commit.
		if err == nil && !sameManualRequest(result.RequestJSON, req) {
			return ErrManualConflict
		}
		return err
	})
	return result, err
}

func sameManualRequest(raw json.RawMessage, req screening.SearchRequest) bool {
	// JSONB changes formatting/key order, including nested provider_config.
	var previous, current any
	encoded, err := json.Marshal(req)
	if err != nil || json.Unmarshal(raw, &previous) != nil || json.Unmarshal(encoded, &current) != nil {
		return false
	}
	a, _ := json.Marshal(previous)
	b, _ := json.Marshal(current)
	return bytes.Equal(a, b)
}

func (s ScreeningService) GetManualSearch(ctx context.Context, tenant, actor, id string) (screening.Details, error) {
	item, err := s.screeningRepo.GetByID(ctx, tenant, id)
	if err != nil {
		return screening.Details{}, err
	}
	// Do not turn this endpoint into a bypass of case/inbox review authorization.
	if !item.IsManual || item.DecisionID != "" || item.ScenarioID != "" || !strings.HasPrefix(item.IdempotencyKey, manualPrefix(actor)) {
		return screening.Details{}, pgx.ErrNoRows
	}
	return s.GetDetails(ctx, tenant, id)
}
