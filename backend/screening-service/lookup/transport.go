package lookup

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/providerproduct"
	"time"
)

// Transport is an optional provider HTTP transport. The public facade keeps
// current platform authority and transactional evidence ownership unchanged.
type Transport struct{ client *providerproduct.Client }

func TransportFromEnvironment(timeout time.Duration) (*Transport, error) {
	client, e := providerproduct.FromEnvironment(timeout)
	if e != nil {
		return nil, e
	}
	if client == nil {
		return nil, nil
	}
	return &Transport{client: client}, nil
}
