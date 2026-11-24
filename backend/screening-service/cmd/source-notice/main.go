// source-notice is an explicit global-operator tool, never a customer endpoint.
// Both separately provisioned DB authority and a signed current human session
// are required. Content approval never grants tenants or approves source rights.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	identity "github.com/Kwasi-itc/New-fraud-system/backend/platform-auth"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/reviewednotices"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceconfig"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourceconnectors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"os"
	"time"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, "reviewed notice operation failed:", e)
		os.Exit(1)
	}
}
func boundedFile(path string, limit int64) ([]byte, error) {
	file, e := os.Open(path)
	if e != nil {
		return nil, fmt.Errorf("input file unavailable")
	}
	defer file.Close()
	info, e := file.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("input file exceeds its limit")
	}
	raw, e := io.ReadAll(io.LimitReader(file, limit+1))
	if e != nil || int64(len(raw)) > limit {
		return nil, fmt.Errorf("input file exceeds its limit")
	}
	return raw, nil
}
func run() error {
	action := flag.String("action", "catalog", "catalog, submit, inspect, approve, reject, recover, export")
	manifest := flag.String("manifest", "", "Explicit UTF-8 JSON manifest, including bounded CSV snapshot")
	attachment := flag.String("attachment", "", "Explicit original PDF or captured plain-text attachment")
	receipt := flag.String("receipt", "", "Import receipt UUID")
	after := flag.String("after", "", "Diff record-ID continuation")
	kind := flag.String("kind", "attachment", "attachment or csv for export")
	output := flag.String("output", "", "New private export file; existing paths are never overwritten")
	flag.Parse()
	if *action == "catalog" {
		return json.NewEncoder(os.Stdout).Encode(sourceconnectors.NoticeRegistry())
	}
	dsn, e := sourceconfig.ReadSecret("SOURCE_DATABASE_URL")
	if e != nil || dsn == "" {
		return fmt.Errorf("explicit source operator database secret is required")
	}
	actorToken, e := sourceconfig.ReadSecret("NOTICE_ACTOR_TOKEN")
	if e != nil || actorToken == "" {
		return fmt.Errorf("explicit signed human session secret is required")
	}
	verifier, e := identity.FromEnvironment()
	if e != nil || verifier == nil {
		return fmt.Errorf("platform verification keys are not configured")
	}
	principal, e := verifier.VerifyPlatformSession(actorToken)
	if e != nil {
		return fmt.Errorf("a valid platform session is required")
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		return fmt.Errorf("invalid operator database configuration")
	}
	cfg.MaxConns = 2
	cfg.MinConns = 0
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "30000"
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = "5000"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	db, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		return fmt.Errorf("operator database unavailable")
	}
	defer db.Close()
	service := reviewednotices.Service{DB: db}
	if *action == "submit" {
		raw, e := boundedFile(*manifest, 12*1024*1024)
		if e != nil {
			return e
		}
		var in reviewednotices.Input
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if dec.Decode(&in) != nil || dec.Decode(new(any)) != io.EOF {
			return fmt.Errorf("invalid notice manifest")
		}
		body, e := boundedFile(*attachment, 10*1024*1024)
		if e != nil {
			return e
		}
		out, e := service.Submit(ctx, principal, in, body)
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(out)
	}
	id, e := uuid.Parse(*receipt)
	if e != nil || id == uuid.Nil {
		return fmt.Errorf("valid receipt UUID required")
	}
	switch *action {
	case "inspect":
		raw, e := service.Inspect(ctx, principal, id, *after)
		if e != nil {
			return e
		}
		_, e = os.Stdout.Write(append(raw, '\n'))
		return e
	case "approve":
		out, e := service.Approve(ctx, principal, id)
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(out)
	case "reject":
		if e = service.Reject(ctx, principal, id); e != nil {
			return e
		}
	case "recover":
		if e = service.Recover(ctx, principal, id); e != nil {
			return e
		}
	case "export":
		if *output == "" {
			return fmt.Errorf("explicit new export path required")
		}
		raw, hash, e := service.Artifact(ctx, principal, id, *kind)
		if e != nil {
			return e
		}
		file, e := os.OpenFile(*output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return fmt.Errorf("export path must be a new writable file")
		}
		_, e = file.Write(raw)
		closeErr := file.Close()
		if e != nil || closeErr != nil {
			return fmt.Errorf("evidence export did not complete")
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"sha256": hash, "bytes": len(raw)})
	default:
		return fmt.Errorf("unsupported notice operation")
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"receipt": id, "action": *action, "complete": true})
}
