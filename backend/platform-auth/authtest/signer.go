package authtest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	auth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
)

func ServiceSigner(t testing.TB, service string) *auth.Signer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return &auth.Signer{Key: key, KeyID: "test-" + service, ServiceID: service}
}
func ServiceTrust(t testing.TB, s *auth.Signer) auth.TrustedKey {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "services.json"))
	if err != nil {
		t.Fatal(err)
	}
	var profiles []struct {
		ID           string   `json:"id"`
		Capabilities []string `json:"capabilities"`
		Audiences    []string `json:"audiences"`
	}
	if err = json.Unmarshal(raw, &profiles); err != nil {
		t.Fatal(err)
	}
	var caps, allowed []string
	for _, profile := range profiles {
		if profile.ID == s.ServiceID {
			caps = profile.Capabilities
			allowed = profile.Audiences
		}
	}
	if len(caps) == 0 {
		t.Fatal("unknown service fixture identity")
	}
	data, err := x509.MarshalPKIXPublicKey(&s.Key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	pub := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: data}))
	return auth.TrustedKey{ID: s.KeyID, PublicKey: pub, Issuer: "urn:fraud:service:" + s.ServiceID, PrincipalTypes: []auth.PrincipalType{auth.PrincipalService}, ServiceID: s.ServiceID, Capabilities: caps, Audiences: allowed}
}
func ServiceVerifier(t testing.TB, s *auth.Signer, audiences ...string) *auth.Verifier {
	t.Helper()
	v, err := auth.NewTrustedVerifier(auth.KeyRing{Keys: []auth.TrustedKey{ServiceTrust(t, s)}}, audiences...)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

type verifiers []auth.TokenVerifier

func (v verifiers) Verify(raw string) (auth.Principal, error) {
	for _, one := range v {
		if p, err := one.Verify(raw); err == nil {
			return p, nil
		}
	}
	return auth.Principal{}, fmt.Errorf("untrusted test token")
}
func MergeVerifiers(values ...auth.TokenVerifier) auth.TokenVerifier { return verifiers(values) }
