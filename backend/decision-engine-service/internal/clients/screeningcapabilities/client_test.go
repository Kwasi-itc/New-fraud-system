package screeningcapabilities

import (
	"context"
	auth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	authtest "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth/authtest"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCapabilityReader(t *testing.T) {
	signer := authtest.ServiceSigner(t, "decision-engine-service")
	verifier := authtest.ServiceVerifier(t, signer, "urn:fraud:screening-service")
	for _, tc := range []struct {
		status int
		body   string
		ok     bool
		failed bool
	}{
		{200, `{"address_supported":true,"birth_date_supported":true,"nationality_supported":true,"country_supported":true,"registration_number_supported":true,"matching_settings_supported":true}`, true, false}, {200, `{"address_supported":false}`, false, false},
		{503, `private error`, false, true}, {200, `invalid`, false, true}, {302, ``, false, true},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, err := verifier.Verify(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
			if r.URL.Path != "/internal/screening-capabilities" || err != nil || !p.Can("screening:intake") {
				t.Error("wrong capability request")
			}
			w.WriteHeader(tc.status)
			w.Write([]byte(tc.body))
		}))
		ok, err := Reader(server.URL, time.Second, signer)(auth.WithOrigin(context.Background(), auth.Origin{TenantID: uuid.New()}))
		server.Close()
		if ok.MatchingSupported != tc.ok || ok.RegistrationSupported != tc.ok || ok.NationalitySupported != tc.ok || ok.CountrySupported != tc.ok || ok.BirthDateSupported != tc.ok || ok.AddressSupported != tc.ok || (err != nil) != tc.failed {
			t.Fatal(ok, err)
		}
	}
}

func TestOlderCapabilityResponseDoesNotEnableBirthDate(t *testing.T) {
	signer := authtest.ServiceSigner(t, "decision-engine-service")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"address_supported":true}`)) }))
	defer server.Close()
	got, err := Reader(server.URL, time.Second, signer)(auth.WithOrigin(context.Background(), auth.Origin{TenantID: uuid.New()}))
	if err != nil || !got.AddressSupported || got.BirthDateSupported || got.NationalitySupported || got.CountrySupported || got.RegistrationSupported || got.MatchingSupported {
		t.Fatal(got, err)
	}
}
