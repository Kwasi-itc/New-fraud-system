package mysqlpublication

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/google/uuid"
	"strings"
)

type localWatchEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

func localName(value string) string { return strings.ToLower(strings.Join(strings.Fields(value), " ")) }

// ProcessLocalBatch executes only explicitly configured local-development
// exact-name watchlists. Pending and processing rows are resumable: computation
// has no external side effects, and publication uses the source version fence.
func (s *Store) ProcessLocalBatch(ctx context.Context, limit int) error {
	if limit < 1 || limit > 100 {
		return ErrInvalid
	}
	type candidate struct {
		id      mysqlstore.UUID
		version int64
		status  string
		raw     []byte
	}
	var items []candidate
	if err := mysqlstore.Snapshot(ctx, s.db, func(tx *sql.Tx) error {
		if err := s.authorize(ctx, tx, s.tenant, false); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, "SELECT id,projection_version,status,CASE WHEN OCTET_LENGTH(request_json)<=4194304 THEN request_json ELSE NULL END FROM "+s.schema+".screening_screenings FORCE INDEX(screening_local_ready) WHERE tenant_id=? AND provider='local-development' AND status IN('pending','processing') ORDER BY status,created_at,id LIMIT ?", mysqlstore.UUID(s.tenant), limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c candidate
			if err := rows.Scan(&c.id, &c.version, &c.status, &c.raw); err != nil {
				return err
			}
			items = append(items, c)
		}
		return rows.Err()
	}); err != nil {
		return err
	}
	for _, item := range items {
		id := uuid.UUID(item.id)
		version := item.version
		if item.status == "pending" {
			var err error
			version, err = s.Start(ctx, id, version)
			if errors.Is(err, ErrConflict) {
				continue
			}
			if err != nil {
				return err
			}
		}
		result, err := localWatchlistResult(id, item.raw)
		if err != nil {
			_, err = s.Fail(ctx, id, version, "Invalid local exact-name watchlist configuration")
		} else {
			_, err = s.Complete(ctx, id, version, result)
		}
		if err != nil && !errors.Is(err, ErrConflict) {
			return err
		}
	}
	return nil
}
func localWatchlistResult(id uuid.UUID, raw []byte) (Result, error) {
	var input struct {
		Queries []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"queries"`
		Config struct {
			Mode      string             `json:"mode"`
			Watchlist *[]localWatchEntry `json:"watchlist"`
		} `json:"provider_config"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &input) != nil || input.Config.Watchlist == nil || len(*input.Config.Watchlist) > 500 || len(input.Queries) == 0 || len(input.Queries) > 500 || input.Config.Mode != "" && input.Config.Mode != "exact" {
		return Result{}, ErrInvalid
	}
	byName := map[string][]localWatchEntry{}
	seen := map[string]bool{}
	for _, e := range *input.Config.Watchlist {
		if e.ID == "" || len(e.ID) > 512 || e.Name == "" || len(e.Name) > 1000 || seen[e.ID] {
			return Result{}, ErrInvalid
		}
		seen[e.ID] = true
		key := localName(e.Name)
		byName[key] = append(byName[key], e)
	}
	result := Result{Reference: "local-watchlist:" + id.String(), Response: json.RawMessage(`{"backend":"local-development-watchlist","match_mode":"exact-name"}`)}
	seen = map[string]bool{}
	for _, q := range input.Queries {
		for _, entry := range byName[localName(q.Name)] {
			if entry.Type != "" && q.Type != "" && entry.Type != q.Type || seen[entry.ID] {
				continue
			}
			seen[entry.ID] = true
			payload, _ := json.Marshal(entry)
			result.Matches = append(result.Matches, Match{ID: uuid.NewSHA1(id, []byte(entry.ID)), EntityID: entry.ID, Provider: "local-development", Name: entry.Name, Status: "pending", Score: 1, Payload: payload})
			if len(result.Matches) > 500 {
				return Result{}, ErrInvalid
			}
		}
	}
	return result, nil
}
