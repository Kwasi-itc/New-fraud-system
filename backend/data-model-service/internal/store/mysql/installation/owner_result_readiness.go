package installation

import (
	"context"
	"database/sql"
	"errors"
	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/store/mysql/migrations"
	"strings"
)

// NewExactKeyReadinessWithOwnerResults binds all 89 source-bound installation
// steps and the three distinct locked dependency definers. No successful check
// is cached; callers retain the execution lock through owner publication.
func NewExactKeyReadinessWithOwnerResults(db *sql.DB, plan migrations.Plan, resolver, creation, result Definer) (*ExactKeyReadiness, error) {
	defs := []Definer{resolver, creation, result}
	for i, d := range defs {
		if _, err := d.SQLAccount(); err != nil {
			return nil, err
		}
		for _, prior := range defs[:i] {
			if d.User == prior.User && strings.EqualFold(d.Host, prior.Host) {
				return nil, errors.New("distinct dependency definers required")
			}
		}
	}
	return newExactKeyReadiness(db, plan, resolver, &creation, &result)
}

func (r *ExactKeyReadiness) verifyOwnerResults(ctx context.Context, conn *sql.Conn, record Definer) error {
	def := *r.resultDefiner
	if record.User == def.User && strings.EqualFold(record.Host, def.Host) {
		return errors.New("record and owner-result definers must be distinct")
	}
	if err := VerifyAccountPolicy(ctx, conn, r.database, OwnerResultDefinerPolicy(def)); err != nil {
		return err
	}
	if err := VerifyNoAccountSessions(ctx, conn, def); err != nil {
		return err
	}
	rows, err := conn.QueryContext(ctx, "SELECT TRIGGER_NAME FROM information_schema.TRIGGERS WHERE EVENT_OBJECT_SCHEMA=? AND EVENT_OBJECT_TABLE='data_model_owner_results' ORDER BY TRIGGER_NAME LIMIT 4", r.database)
	if err != nil {
		return err
	}
	want := map[string]bool{"data_model_owner_results_insert_guard": true, "data_model_owner_results_update_guard": true, "data_model_owner_results_delete_guard": true}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			break
		}
		if !want[name] {
			err = migrations.ErrDrift
			break
		}
		delete(want, name)
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	if err != nil {
		return err
	}
	if len(want) != 0 {
		return migrations.ErrDrift
	}
	return nil
}
