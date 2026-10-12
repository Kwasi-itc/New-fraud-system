// oidc-check validates provider discovery without printing any credentials.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/auth-service/internal/oidcprovider"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	p, err := oidcprovider.New(ctx, oidcprovider.FromEnvironment(os.Getenv))
	if err != nil {
		fmt.Fprintln(os.Stderr, "oidc-check:", err)
		os.Exit(1)
	}
	if err = json.NewEncoder(os.Stdout).Encode(map[string]any{"issuer": p.Issuer(), "discovery_verified": true, "browser_login_checked": false}); err != nil {
		os.Exit(1)
	}
}
