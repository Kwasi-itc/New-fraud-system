package mysqlruntime

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/clients/provider"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/localmatching"
)

// The worker and synchronous decision evaluator share the screening-owned matcher.
type LocalProvider = localmatching.LocalProvider

func LocalCapabilities() provider.ManualCapabilities { return localmatching.LocalCapabilities() }
