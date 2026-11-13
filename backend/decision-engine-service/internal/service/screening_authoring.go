package service

import (
	"context"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/workflow"
)

type ScreeningAuthoringService struct {
	NameRecognitionSupported bool
	Capabilities             func(context.Context) (screening.AuthoringCapabilities, error)
	Store                    ScreeningAuthoringStore
	Cache                    DecisionMetadataCacheInvalidator
}

func (s ScreeningAuthoringService) List(ctx context.Context, t, sc string, a workflow.AuthoringActor) (screening.AuthoringPage, error) {
	out, err := s.Store.List(ctx, t, sc, a)
	out.NameRecognitionSupported = s.NameRecognitionSupported
	if err == nil && s.Capabilities != nil {
		out.AuthoringCapabilities, _ = s.Capabilities(ctx)
	}
	return out, err
}
func (s ScreeningAuthoringService) Save(ctx context.Context, t, sc string, a workflow.AuthoringActor, r screening.AuthoringRule) (screening.AuthoringRule, error) {
	if r.ForcedOutcome != "" {
		if s.Capabilities == nil {
			return screening.AuthoringRule{}, screening.ErrInlineUnavailable
		}
		cap, err := s.Capabilities(ctx)
		if err != nil || !cap.InlineSupported {
			return screening.AuthoringRule{}, screening.ErrInlineUnavailable
		}
	}
	if r.UseNER && !s.NameRecognitionSupported {
		return screening.AuthoringRule{}, screening.ErrRecognitionUnavailable
	}
	if r.UseNER || r.Threshold != nil || r.ResultLimit != nil || len(r.Datasets) > 0 || r.AddressField != "" || r.BirthDateField != "" || r.NationalityField != "" || r.CountryField != "" || r.RegistrationField != "" {
		cap := screening.AuthoringCapabilities{}
		if s.Capabilities != nil {
			var err error
			cap, err = s.Capabilities(ctx)
			if err != nil {
				cap = screening.AuthoringCapabilities{}
			}
		}
		if (r.UseNER && !cap.MatchingSupported) || (r.Threshold != nil && !cap.MatchingSupported && !cap.ThresholdSupported) || (r.ResultLimit != nil && !cap.MatchingSupported && !cap.ResultLimitSupported) || (len(r.Datasets) > 0 && !cap.MatchingSupported && !cap.DatasetSelectionSupported) {
			return screening.AuthoringRule{}, screening.ErrMatchingUnavailable
		}
		if r.AddressField != "" && !cap.AddressSupported {
			return screening.AuthoringRule{}, screening.ErrAddressUnavailable
		}
		if r.BirthDateField != "" && !cap.BirthDateSupported {
			return screening.AuthoringRule{}, screening.ErrBirthDateUnavailable
		}
		if r.NationalityField != "" && !cap.NationalitySupported {
			return screening.AuthoringRule{}, screening.ErrNationalityUnavailable
		}
		if r.CountryField != "" && !cap.CountrySupported {
			return screening.AuthoringRule{}, screening.ErrCountryUnavailable
		}

		if r.RegistrationField != "" && !cap.RegistrationSupported {
			return screening.AuthoringRule{}, screening.ErrRegistrationUnavailable
		}

	}

	out, err := s.Store.Save(ctx, t, sc, a, r)
	if err == nil && s.Cache != nil {
		s.Cache.InvalidateActiveScreeningConfigs(ctx, t, sc)
	}
	return out, err
}

type ScreeningAuthoringStore interface {
	List(context.Context, string, string, workflow.AuthoringActor) (screening.AuthoringPage, error)
	Save(context.Context, string, string, workflow.AuthoringActor, screening.AuthoringRule) (screening.AuthoringRule, error)
}
