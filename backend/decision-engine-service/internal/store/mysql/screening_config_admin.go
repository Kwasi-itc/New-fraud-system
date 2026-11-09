package mysql

import (
	"context"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/google/uuid"
)

func screeningConfigRepo(r authoringRepository) ScreeningConfigRepository {
	return ScreeningConfigRepository{configurationRepository{r, screeningConfiguration}}
}
func (s *AdministrationStore) ScreeningConfigs(ctx context.Context, parent uuid.UUID) ([]screening.Config, error) {
	return adminRun(ctx, s, false, AuthoringRead, func(r authoringRepository) ([]screening.Config, error) {
		if _, err := (ScenarioRepository{r}).GetByID(ctx, r.tenant.String(), parent.String()); err != nil {
			return nil, err
		}
		return screeningConfigRepo(r).ListByScenario(ctx, r.tenant.String(), parent.String())
	})
}
func (s *AdministrationStore) ScreeningConfig(ctx context.Context, parent, id uuid.UUID) (screening.Config, error) {
	return adminRun(ctx, s, false, AuthoringRead, func(r authoringRepository) (screening.Config, error) {
		return screeningConfigRepo(r).GetByID(ctx, r.tenant.String(), parent.String(), id.String())
	})
}
func (s *AdministrationStore) SaveScreeningConfig(ctx context.Context, parent, id uuid.UUID, fields screening.Config) (screening.Config, error) {
	return adminRun(ctx, s, true, AuthoringWrite, func(r authoringRepository) (screening.Config, error) {
		if _, err := (ScenarioRepository{r}).GetByID(ctx, r.tenant.String(), parent.String()); err != nil {
			return screening.Config{}, err
		}
		repo := screeningConfigRepo(r)
		create := id == uuid.Nil
		if create {
			allocated, err := s.identity()
			if err != nil {
				return screening.Config{}, err
			}
			fields.ID = allocated
			fields.CreatedAt = s.now()
		} else {
			current, err := repo.GetByID(ctx, r.tenant.String(), parent.String(), id.String())
			if err != nil {
				return screening.Config{}, err
			}
			fields.ID = current.ID
			fields.CreatedAt = current.CreatedAt
		}
		fields.TenantID, fields.ScenarioID, fields.UpdatedAt = r.tenant.String(), parent.String(), s.now()
		if err := fields.Validate(); err != nil {
			return screening.Config{}, errors.Join(ErrAdministrationInput, err)
		}
		if fields.ReviewInboxID != nil {
			if err := s.inboxes.Authorize(ctx, r.tx, r.tenant, uuid.MustParse(*fields.ReviewInboxID), true, fields.Active); err != nil {
				return screening.Config{}, err
			}
		}
		if create {
			return repo.Create(ctx, fields)
		}
		return repo.Update(ctx, fields)
	})
}
func (s *AdministrationStore) DeleteScreeningConfig(ctx context.Context, parent, id uuid.UUID) error {
	_, err := adminRun(ctx, s, true, AuthoringWrite, func(r authoringRepository) (struct{}, error) {
		return struct{}{}, screeningConfigRepo(r).Delete(ctx, r.tenant.String(), parent.String(), id.String())
	})
	return err
}
