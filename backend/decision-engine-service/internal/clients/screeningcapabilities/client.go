package screeningcapabilities

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/screening"
	platformauth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"io"
	"net/http"
	"strings"
	"time"
)

// Returns only an advertised capability, never provider health. No provider call.
func Reader(base string, timeout time.Duration, signers ...*platformauth.Signer) func(context.Context) (screening.AuthoringCapabilities, error) {
	if timeout <= 0 || timeout > 5*time.Second {
		timeout = 5 * time.Second
	}
	var signer *platformauth.Signer
	if len(signers) > 0 {
		signer = signers[0]
	}
	client := &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return func(ctx context.Context) (screening.AuthoringCapabilities, error) {
		if base == "" {
			return screening.AuthoringCapabilities{}, fmt.Errorf("Screening capabilities unavailable.")
		}
		req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(base, "/")+"/internal/screening-capabilities", nil)
		if err != nil {
			return screening.AuthoringCapabilities{}, fmt.Errorf("Screening capabilities unavailable.")
		}
		tenant, authErr := platformauth.RequestTenant(req)
		if authErr != nil {
			return screening.AuthoringCapabilities{}, authErr
		}
		if authErr = platformauth.AuthorizeServiceRequest(req, tenant, "urn:fraud:screening-service", []string{"screening:intake"}, signer); authErr != nil {
			return screening.AuthoringCapabilities{}, authErr
		}
		response, err := client.Do(req)
		if err != nil {
			return screening.AuthoringCapabilities{}, fmt.Errorf("Screening capabilities unavailable.")
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			return screening.AuthoringCapabilities{}, fmt.Errorf("Screening capabilities unavailable.")
		}
		var result screening.AuthoringCapabilities
		if json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&result) != nil {
			return screening.AuthoringCapabilities{}, fmt.Errorf("Screening capabilities unavailable.")
		}
		return result, nil
	}
}
