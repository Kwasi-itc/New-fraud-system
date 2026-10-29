// Package migrations runs reviewed, owner-scoped MySQL DDL plans. It never
// assumes DDL is transactional and never repairs drift by dropping objects.
package migrations

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

// Step contains one server statement, including any complete routine body.
// Before and After are canonical Definition JSON from the pinned server version,
// including definers, SQL security, constraints and table options. Empty Before
// means absent. A plan is trusted deployment code, never an HTTP input.
// SQL must use the selected shared database explicitly. Do not include USE,
// transaction/session statements, lock manipulation or multiple statements.
type Step struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	SQL    string `json:"sql"`
	Before string `json:"before"`
	After  string `json:"after"`
}

type Plan struct {
	Database string `json:"database"`
	Steps    []Step `json:"steps"`
}

func (p Plan) Validate() error {
	if _, err := mysqlstore.Identifier(p.Database); err != nil {
		return err
	}
	if len(p.Steps) == 0 || len(p.Steps) > 10000 {
		return errors.New("migration plan requires 1..10000 steps")
	}
	seen := make(map[string]bool, len(p.Steps))
	latest := make(map[string]string)
	for i, s := range p.Steps {
		if _, err := mysqlstore.Identifier(s.ID); err != nil {
			return fmt.Errorf("step %d ID: %w", i+1, err)
		}
		if _, err := mysqlstore.Identifier(s.Name); err != nil {
			return fmt.Errorf("step %d object: %w", i+1, err)
		}
		if seen[s.ID] || s.Name == "data_model_schema_migrations" {
			return errors.New("duplicate step ID or reserved migration ledger")
		}
		seen[s.ID] = true
		switch s.Kind {
		case "TABLE", "VIEW", "PROCEDURE", "FUNCTION", "TRIGGER":
		default:
			return errors.New("unsupported migration object kind")
		}
		if len(s.SQL) == 0 || len(s.SQL) > 1<<20 || len(s.Before) > 1<<20 || len(s.After) == 0 || len(s.After) > 1<<20 || s.Before == s.After {
			return errors.New("bounded nonempty SQL and distinct before/after definitions required")
		}
		for _, definition := range []string{s.Before, s.After} {
			if definition == "" {
				continue
			}
			var fields map[string]string
			if err := json.Unmarshal([]byte(definition), &fields); err != nil || len(fields) < 2 {
				return errors.New("canonical object definition JSON required")
			}
			canonical, _ := json.Marshal(fields)
			if string(canonical) != definition {
				return errors.New("object definition must use canonical JSON")
			}
		}
		// This is validation of a reviewed artifact, not an arbitrary SQL sandbox.
		prefix := strings.ToUpper(strings.TrimSpace(s.SQL))
		if !strings.HasPrefix(prefix, "CREATE ") && !strings.HasPrefix(prefix, "ALTER ") {
			return errors.New("only CREATE/ALTER migration statements supported")
		}
		key := s.Kind + ":" + s.Name
		if previous, ok := latest[key]; ok && previous != s.Before {
			return fmt.Errorf("step %s breaks object definition chain", s.ID)
		}
		latest[key] = s.After
	}
	return nil
}

func checksum(database string, s Step) [32]byte {
	// Struct serialization is deterministic; field boundaries cannot collide.
	data, _ := json.Marshal(struct {
		Database string
		Step     Step
	}{database, s})
	return sha256.Sum256(data)
}
