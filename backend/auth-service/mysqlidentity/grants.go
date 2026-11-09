package mysqlidentity

import "github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"

// RuntimeGrants grants verification reads and only the public decision action
// function. It permits no identity mutations and no worker impersonation.
func RuntimeGrants(database, user string) ([]string, error) {
	s, e := mysqlstore.Identifier(database)
	if e != nil {
		return nil, e
	}
	u, e := mysqlstore.Identifier(user)
	if e != nil {
		return nil, e
	}
	account := u + "@'%'"
	return []string{
		"GRANT EXECUTE ON FUNCTION " + s + ".identity_authorize_decision_v1 TO " + account,
		"GRANT SELECT(id,digest,user_id,tenant_id,read_only,expires_at,idle_until,revoked_at) ON " + s + ".identity_sessions TO " + account,
		"GRANT SELECT(id,active) ON " + s + ".identity_users TO " + account,
		"GRANT SELECT(tenant_id,user_id,version,active) ON " + s + ".identity_memberships TO " + account,
		"GRANT SELECT(id,active) ON " + s + ".identity_tenants TO " + account,
	}, nil
}
