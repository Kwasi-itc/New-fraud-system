package app

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/internal/access"
	auth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
)

func (c Config) APIAuth() (*access.Verifier, error) {
	v, err := auth.FromEnvironment()
	if err != nil {
		return nil, err
	}
	return access.NewPlatformVerifier(v), nil
}
