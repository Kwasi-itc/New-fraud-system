package mysqlauth

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strconv"

	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

// IssuerMoveResult describes an operator-only, offline identity-provider move.
type IssuerMoveResult struct {
	Identities    int64 `json:"identities"`
	Invitations   int64 `json:"invitations"`
	ExpiredLogins int64 `json:"expired_logins"`
}

func localIssuer(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.Hostname() != "localhost" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
		return false
	}
	port, err := strconv.Atoi(u.Port())
	return err == nil && port >= 1024 && port <= 65535 && (u.Path == "/realms/fraud" || u.Path == "/identity/realms/fraud")
}

// MoveLocalIssuer changes the exact issuer/subject keys, never user IDs or
// memberships. The local operator must stop auth API/worker writers first.
// A bounded catalog lock and collision preflight make the operation atomic;
// retrying a completed move is a no-op. In-flight old-issuer logins are removed
// because their authorization codes belong to the previous public endpoint.
func (s *Store) MoveLocalIssuer(ctx context.Context, from, to string) (IssuerMoveResult, error) {
	var result IssuerMoveResult
	if !localIssuer(from) || !localIssuer(to) {
		return result, errors.New("issuer moves require explicit local fraud realm URLs")
	}
	if from == to {
		return result, nil
	}
	err := s.transaction(ctx, func(tx *sql.Tx) error {
		type entry struct {
			key             []byte
			issuer, subject string
			user            mysqlstore.UUID
		}
		rows, err := tx.QueryContext(ctx, "SELECT identity_key,issuer,subject,user_id FROM "+s.table("identity_external_identities")+" ORDER BY identity_key LIMIT 10001 FOR UPDATE")
		if err != nil {
			return err
		}
		var entries []entry
		keys := map[string]bool{}
		for rows.Next() {
			var value entry
			if err = rows.Scan(&value.key, &value.issuer, &value.subject, &value.user); err != nil {
				rows.Close()
				return err
			}
			entries = append(entries, value)
			keys[string(value.key)] = true
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(entries) > 10000 {
			return errors.New("local issuer move exceeds its identity bound")
		}
		for _, table := range []string{"identity_invitations", "identity_login_transactions"} {
			var count int
			if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM (SELECT issuer FROM "+s.table(table)+" WHERE issuer=? LIMIT 10001) pending", from).Scan(&count); err != nil {
				return err
			}
			if count > 10000 {
				return errors.New("local issuer move exceeds its pending-operation bound")
			}
		}
		for _, value := range entries {
			if value.issuer != from {
				continue
			}
			oldKey, err := identityKey(from, value.subject)
			if err != nil || !bytes.Equal(oldKey[:], value.key) {
				return errors.New("stored identity key does not match its issuer and subject")
			}
			newKey, err := identityKey(to, value.subject)
			if err != nil {
				return err
			}
			if keys[string(newKey[:])] {
				return conflict
			}
		}
		for _, value := range entries {
			if value.issuer != from {
				continue
			}
			newKey, _ := identityKey(to, value.subject)
			if _, err = tx.ExecContext(ctx, "INSERT INTO "+s.table("identity_auth_external_guards")+"(identity_key) VALUES(?) ON DUPLICATE KEY UPDATE identity_key=VALUES(identity_key)", newKey[:]); err != nil {
				return err
			}
			updated, err := tx.ExecContext(ctx, "UPDATE "+s.table("identity_external_identities")+" SET identity_key=?,issuer=? WHERE identity_key=? AND issuer=? AND user_id=?", newKey[:], to, value.key, from, value.user)
			if err != nil {
				return err
			}
			n, err := updated.RowsAffected()
			if err != nil {
				return err
			}
			if n != 1 {
				return conflict
			}
			result.Identities += n
		}
		// Invitation payload encryption binds the invitation ID, not the issuer;
		// retaining its token/digest also retains already-sent invitation links.
		updated, err := tx.ExecContext(ctx, "UPDATE "+s.table("identity_invitations")+" SET issuer=? WHERE issuer=?", to, from)
		if err != nil {
			return err
		}
		result.Invitations, err = updated.RowsAffected()
		if err != nil {
			return err
		}
		deleted, err := tx.ExecContext(ctx, "DELETE FROM "+s.table("identity_login_transactions")+" WHERE issuer=?", from)
		if err != nil {
			return err
		}
		result.ExpiredLogins, err = deleted.RowsAffected()
		return err
	})
	if err != nil {
		return IssuerMoveResult{}, err
	}
	return result, nil
}
