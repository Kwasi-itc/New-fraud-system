package installation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// InstallLockedAccountPolicies installs reviewed grants on existing, locked,
// unused accounts. The caller must hold the migration lock on conn and exclude
// concurrent privilege administration. GRANT is not transactional: failures may
// leave a prefix applied. Replaying the identical policy safely resumes that
// prefix, including a lost acknowledgement. This never creates/unlocks accounts,
// changes credentials, revokes privileges or silently adopts unexpected grants.
func InstallLockedAccountPolicies(ctx context.Context, conn *sql.Conn, database string, policies []AccountPolicy) error {
	if conn == nil || len(policies) == 0 || len(policies) > 64 {
		return errors.New("pinned installation connection and 1..64 account policies required")
	}
	all := make([][]string, len(policies))
	seen := map[string]bool{}
	bytes := 0
	// Validate the entire document before inspecting or changing any account.
	for i, policy := range policies {
		key := policy.User + "\x00" + policy.Host
		if !policy.Locked || seen[key] {
			return errors.New("distinct locked account policies required for installation")
		}
		seen[key] = true
		grants, err := GrantStatements(database, policy)
		if err != nil {
			return err
		}
		for _, grant := range grants {
			bytes += len(grant)
		}
		if bytes > 1<<20 {
			return errors.New("combined installation grants exceed 1 MiB")
		}
		all[i] = grants
	}
	// Preflight every account first, so an unsafe later account cannot cause
	// changes to earlier accounts. ACCOUNT LOCK alone does not end old sessions.
	var performanceSchema bool
	if err := conn.QueryRowContext(ctx, "SELECT @@GLOBAL.performance_schema").Scan(&performanceSchema); err != nil {
		return err
	}
	if !performanceSchema {
		return errors.New("performance_schema required to inspect existing account sessions")
	}
	for _, policy := range policies {
		if err := inspectAccountPolicy(ctx, conn, database, policy, true); err != nil {
			return fmt.Errorf("preflight account %s@%s: %w", policy.User, policy.Host, err)
		}
		var sessions int
		if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM performance_schema.threads WHERE TYPE='FOREGROUND' AND PROCESSLIST_USER=?", policy.User).Scan(&sessions); err != nil {
			return err
		}
		if sessions != 0 {
			return fmt.Errorf("account %s has existing sessions; locked unused accounts required", policy.User)
		}
	}
	for i, policy := range policies {
		for j, grant := range all[i] {
			if _, err := conn.ExecContext(ctx, grant); err != nil {
				return fmt.Errorf("install account %s@%s grant %d; a prefix may be applied; replay the same reviewed policy: %w", policy.User, policy.Host, j+1, err)
			}
		}
	}
	for _, policy := range policies {
		if err := VerifyAccountPolicy(ctx, conn, database, policy); err != nil {
			return fmt.Errorf("verify installed account %s@%s: %w", policy.User, policy.Host, err)
		}
	}
	return nil
}
