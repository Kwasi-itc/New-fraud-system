package platform

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	auth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func TestClientProofScriptMatchesServerVerifier(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is required for client proof CLI interoperability")
	}
	script := filepath.Join("..", "..", "scripts", "client-proof.mjs")
	dir := filepath.Join(t.TempDir(), "customer-key")
	run := func(args ...string) []byte {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, node, append([]string{script}, args...)...).Output()
		if err != nil {
			t.Fatalf("client proof CLI failed: %v", err)
		}
		return out
	}
	run("keygen", "--dir", dir, "--kid", "customer-test")
	publicPEM, err := os.ReadFile(filepath.Join(dir, "public.pem"))
	if err != nil {
		t.Fatal(err)
	}
	publicKey, err := jwt.ParseRSAPublicKeyFromPEM(publicPEM)
	if err != nil {
		t.Fatal(err)
	}
	client, endpoint := uuid.NewString(), "http://localhost:8088/oauth/token"
	var previous string
	for range 2 {
		output := run("proof", "--dir", dir, "--client-id", client, "--token-url", endpoint)
		raw := strings.TrimSpace(string(output))
		claims, err := auth.VerifyClientAssertion(raw, client, endpoint, "customer-test", publicKey, time.Now())
		if err != nil {
			t.Fatalf("server rejected the generated assertion: %v", err)
		}
		if claims.ID == previous || claims.ExpiresAt.Sub(claims.IssuedAt.Time) != time.Minute {
			t.Fatal("proof did not renew its single-use ID or enforce the 60-second lifetime")
		}
		previous = claims.ID
	}
}
