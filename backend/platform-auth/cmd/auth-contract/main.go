// Command auth-contract prints the canonical capability catalog and explicit
// predefined bundles for documentation and deployment review.
package main

import (
	"encoding/json"
	auth "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"os"
)

func main() {
	out := json.NewEncoder(os.Stdout)
	out.SetIndent("", "  ")
	if err := out.Encode(struct {
		Capabilities []auth.Capability   `json:"capabilities"`
		Roles        map[string][]string `json:"roles"`
	}{auth.Catalog(), auth.PredefinedRoles()}); err != nil {
		panic(err)
	}
}
