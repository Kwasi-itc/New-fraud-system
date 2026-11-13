package screeningcapabilities

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCapabilityReader(t *testing.T) {
	token := "synthetic-intake-private-token-32bytes"
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
			if r.URL.Path != "/internal/screening-capabilities" || r.Header.Get("Authorization") != "Bearer "+token {
				t.Error("wrong capability request")
			}
			w.WriteHeader(tc.status)
			w.Write([]byte(tc.body))
		}))
		ok, err := Reader(server.URL, token, time.Second)(context.Background())
		server.Close()
		if ok.MatchingSupported != tc.ok || ok.RegistrationSupported != tc.ok || ok.NationalitySupported != tc.ok || ok.CountrySupported != tc.ok || ok.BirthDateSupported != tc.ok || ok.AddressSupported != tc.ok || (err != nil) != tc.failed {
			t.Fatal(ok, err)
		}
	}
}

func TestOlderCapabilityResponseDoesNotEnableBirthDate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"address_supported":true}`)) }))
	defer server.Close()
	got, err := Reader(server.URL, "synthetic-intake-private-token-32bytes", time.Second)(context.Background())
	if err != nil || !got.AddressSupported || got.BirthDateSupported || got.NationalitySupported || got.CountrySupported || got.RegistrationSupported || got.MatchingSupported {
		t.Fatal(got, err)
	}
}
