// provider-admin is a private DB-owner tool, never a public HTTP administration API.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/providerproduct"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceconfig"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
	"github.com/google/uuid"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, "provider administration failed:", e)
		os.Exit(1)
	}
}
func run() error {
	action := flag.String("action", "status", "bootstrap, provision-login, status, create-client, issue-key, grant-dataset, revoke-dataset, revoke-key or revoke-client")
	login := flag.String("login", "", "Explicit provider_ service login name")
	capability := flag.String("capability", "screening_provider_reader", "screening_provider_reader or screening_provider_collector")
	name := flag.String("name", "", "Client display name")
	namespace := flag.String("namespace", "", "Independent namespace UUID; existing tenant UUID in shared mode")
	client := flag.String("client", "", "Client UUID")
	credential := flag.String("credential", "", "Credential UUID")
	operations := flag.String("operations", "search,enrich,catalog,freshness,delta", "Explicit client operations")
	budget := flag.Int("requests-per-minute", 6000, "Shared client request budget, 1..60000")
	ttl := flag.Duration("ttl", 720*time.Hour, "Client/key validity, at most 8760h")
	output := flag.String("key-file", "", "One-time credential output path; must not exist; no token is printed")
	scope := flag.String("scope", "global", "global or tenant")
	dataset := flag.String("dataset", "", "Exact dataset ID")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db, e := providerproduct.OpenDatabase(ctx)
	if e != nil {
		return e
	}
	defer db.Close()
	if *action == "bootstrap" {
		if e = providerproduct.Bootstrap(ctx, db); e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"catalog": "standalone", "status": "installed", "sources_enabled": false})
	}
	topology, e := providerproduct.Topology(ctx, db)
	if e != nil {
		return e
	}
	if *action == "provision-login" {
		password, e := sourceconfig.ReadSecret("PROVIDER_LOGIN_PASSWORD")
		if e != nil || password == "" {
			return fmt.Errorf("PROVIDER_LOGIN_PASSWORD_FILE is required")
		}
		if e = providerproduct.ProvisionLogin(ctx, db, *login, password, *capability); e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"login": *login, "capability": *capability, "status": "verified", "password_printed": false})
	}
	if *action == "status" {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"topology": topology, "contract_version": 1})
	}
	service := providerproduct.Credentials{DB: db}
	clientID := uuid.Nil
	if *action != "create-client" {
		clientID, e = uuid.Parse(*client)
		if e != nil || clientID == uuid.Nil {
			return fmt.Errorf("valid client UUID required")
		}
	}
	switch *action {
	case "create-client":
		ns, e := uuid.Parse(*namespace)
		if e != nil {
			return fmt.Errorf("valid namespace UUID required")
		}
		if *ttl < time.Minute || *ttl > 365*24*time.Hour {
			return fmt.Errorf("client validity must be 1m..8760h")
		}
		id, e := service.Create(ctx, providerproduct.ClientInput{Name: *name, Namespace: ns, Operations: strings.Split(*operations, ","), RequestsPerMinute: *budget, ExpiresAt: time.Now().Add(*ttl)})
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"client_id": id, "namespace_id": ns, "credentials_issued": false})
	case "issue-key":
		if strings.TrimSpace(*output) == "" {
			return fmt.Errorf("explicit key-file required")
		}
		created := false
		id, e := service.Issue(ctx, clientID, *ttl, func(token string) error {
			f, e := os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if e != nil {
				return fmt.Errorf("credential output must be a new writable protected file")
			}
			created = true
			if _, e = f.WriteString(token + "\n"); e != nil {
				f.Close()
				return e
			}
			return f.Close()
		})
		if e != nil {
			if created {
				if cleanup := os.Remove(*output); cleanup != nil {
					return fmt.Errorf("credential issuance failed; remove the unusable output file manually")
				}
			}
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"client_id": clientID, "credential_id": id, "key_file": *output, "token_printed": false})
	case "grant-dataset", "revoke-dataset":
		e = service.Grant(ctx, clientID, sourcedata.DatasetRef{Scope: *scope, ID: *dataset}, *action == "grant-dataset")
	case "revoke-key":
		key, e := uuid.Parse(*credential)
		if e != nil || key == uuid.Nil {
			return fmt.Errorf("valid credential UUID required")
		}
		if e = service.Revoke(ctx, clientID, key); e != nil {
			return e
		}
	case "revoke-client":
		e = service.Revoke(ctx, clientID, uuid.Nil)
	default:
		return fmt.Errorf("unsupported provider administration action")
	}
	if e != nil {
		return e
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"client_id": clientID, "action": *action, "status": "complete"})
}
