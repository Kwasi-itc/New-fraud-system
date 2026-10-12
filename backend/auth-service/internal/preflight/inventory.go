// Package preflight inventories existing attribution without creating identities
// or changing owner data. Run with authorized, read-only operator credentials.
package preflight

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type source struct {
	table, key, subject, predicate string
}

// SQL identifiers come only from this allowlist, never command-line input.
var sources = map[string]source{
	"inbox_members":                 {"case_manager.inbox_users", "id", "user_id", ""},
	"case_assignees":                {"case_manager.cases", "id", "assigned_to", ""},
	"case_contributors":             {"case_manager.case_contributors", "id", "user_id", ""},
	"case_event_actors":             {"case_manager.case_events", "id", "user_id", ""},
	"assignment_targets":            {"case_manager.case_events", "id", "new_value", " AND event_type='case_assigned'"},
	"assignment_previous":           {"case_manager.case_events", "id", "previous_value", " AND event_type='case_assigned'"},
	"inbox_event_actors":            {"case_manager.inbox_events", "id", "actor_id", ""},
	"file_uploaders":                {"case_manager.case_files", "id", "uploaded_by", ""},
	"report_creators":               {"case_manager.suspicious_activity_reports", "id", "created_by", ""},
	"report_completers":             {"case_manager.suspicious_activity_reports", "id", "completed_by", ""},
	"decision_review_actors":        {"core.investigator_reviews", "operation_id", "actor_id", ""},
	"screening_review_actors":       {"screening.investigator_reviews", "operation_id", "actor_id", ""},
	"screening_comment_authors":     {"screening.screening_match_comments", "id", "author_id", ""},
	"screening_whitelist_reviewers": {"screening.screening_whitelist_entries", "id", "reviewer_id", ""},
}

func Sources() []string {
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

type Request struct {
	TenantID uuid.UUID
	Source   string
	After    *uuid.UUID
	Limit    int
}

func (r Request) Validate() error {
	if r.TenantID == uuid.Nil {
		return errors.New("a nonzero tenant UUID is required")
	}
	if _, ok := sources[r.Source]; !ok {
		return errors.New("unknown source; use -list-sources")
	}
	if r.Limit < 1 || r.Limit > 1000 {
		return errors.New("limit must be between 1 and 1000")
	}
	return nil
}

type Reference struct {
	RecordID uuid.UUID `json:"record_id"`
	Subject  *string   `json:"subject"`
	// No nonempty subject is considered a verified account, even UUIDs or
	// legacy_import. Preserve exact strings for reviewed reconciliation.
	Classification string `json:"classification"`
}

type Page struct {
	Version      int         `json:"version"`
	TenantID     uuid.UUID   `json:"tenant_id"`
	TenantStatus string      `json:"tenant_status"`
	Source       string      `json:"source"`
	SnapshotAt   time.Time   `json:"snapshot_at"`
	References   []Reference `json:"references"`
	HasMore      bool        `json:"has_more"`
	NextAfter    *uuid.UUID  `json:"next_after,omitempty"`
}

func classify(subject *string) string {
	if subject == nil {
		return "absent"
	}
	if strings.TrimSpace(*subject) == "" {
		return "blank"
	}
	if strings.TrimSpace(*subject) != *subject {
		return "noncanonical"
	}
	return "unresolved"
}

func query(r Request) string {
	s := sources[r.Source]
	filter := ""
	if r.After != nil {
		filter = " AND " + s.key + " > $3::uuid"
	}
	return fmt.Sprintf("SELECT %s, %s FROM %s WHERE tenant_id=$1%s%s ORDER BY %s LIMIT $2",
		s.key, s.subject, s.table, s.predicate, filter, s.key)
}

// ReadPage uses a single read-only snapshot and returns no partial result on
// failure. Pages from separate calls do not share a snapshot: quiesce writers
// for a migration-grade census. No runtime authorization decision uses this tool.
func ReadPage(ctx context.Context, conn *pgx.Conn, r Request) (Page, error) {
	if err := r.Validate(); err != nil {
		return Page{}, err
	}
	if ctx.Err() != nil {
		return Page{}, errors.New("inventory canceled before reading")
	}
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Page{}, errors.New("cannot start read-only inventory")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout='30s'; SET LOCAL lock_timeout='2s'; SET LOCAL idle_in_transaction_session_timeout='30s'"); err != nil {
		return Page{}, errors.New("cannot set inventory limits")
	}
	p := Page{Version: 1, TenantID: r.TenantID, Source: r.Source, References: []Reference{}}
	if err := tx.QueryRow(ctx, "SELECT status, transaction_timestamp() FROM core.tenants WHERE id=$1", r.TenantID).Scan(&p.TenantStatus, &p.SnapshotAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Page{}, errors.New("tenant does not exist")
		}
		return Page{}, errors.New("cannot read tenant registry; check read grants and Data Model migrations")
	}
	args := []any{r.TenantID, r.Limit + 1}
	if r.After != nil {
		args = append(args, *r.After)
	}
	rows, err := tx.Query(ctx, query(r), args...)
	if err != nil {
		return Page{}, errors.New("cannot read source; check owner migrations and read grants")
	}
	defer rows.Close()
	for rows.Next() {
		var ref Reference
		if err := rows.Scan(&ref.RecordID, &ref.Subject); err != nil {
			return Page{}, errors.New("invalid source row")
		}
		ref.Classification = classify(ref.Subject)
		p.References = append(p.References, ref)
	}
	if rows.Err() != nil {
		return Page{}, errors.New("inventory query failed or timed out; no complete page available")
	}
	if len(p.References) > r.Limit {
		p.HasMore = true
		p.References = p.References[:r.Limit]
		last := p.References[len(p.References)-1].RecordID
		p.NextAfter = &last
	}
	if err := tx.Commit(ctx); err != nil {
		return Page{}, errors.New("inventory snapshot could not complete")
	}
	return p, nil
}
