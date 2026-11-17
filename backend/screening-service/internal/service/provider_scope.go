package service

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/providerregistry"
	"github.com/jackc/pgx/v5"
)

func (s ScreeningService) validateProvider(ctx context.Context, tenant string, request screening.SearchRequest) error {
	if validator, ok := s.provider.(interface {
		Validate(context.Context, string, screening.SearchRequest) error
	}); ok {
		return validator.Validate(ctx, tenant, request)
	}
	return nil
}
func (s ScreeningService) ProviderCapabilities(ctx context.Context) ([]providerregistry.Capabilities, error) {
	if provider, ok := s.provider.(interface {
		Capabilities(context.Context) ([]providerregistry.Capabilities, error)
	}); ok {
		return provider.Capabilities(ctx)
	}
	return nil, nil
}
func (s ScreeningService) EnrichManualSearch(ctx context.Context, tenant, actor, id, matchID string) (screening.Match, error) {
	details, err := s.GetManualSearch(ctx, tenant, actor, id)
	if err != nil {
		return screening.Match{}, err
	}
	for _, match := range details.Matches {
		if match.ID == matchID {
			return s.EnrichMatch(ctx, tenant, matchID)
		}
	}
	return screening.Match{}, pgx.ErrNoRows
}
