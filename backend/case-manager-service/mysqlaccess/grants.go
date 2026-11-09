package mysqlaccess

import "github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"

func RuntimeGrants(database, user string) ([]string, error) {
	s, e := mysqlstore.Identifier(database)
	if e != nil {
		return nil, e
	}
	u, e := mysqlstore.Identifier(user)
	if e != nil {
		return nil, e
	}
	var out []string
	for _, name := range []string{"case_manager_authorize_decision_inbox_v1", "case_manager_authorize_decision_inboxes_v1", "case_manager_authorize_decision_tags_v1", "case_manager_visible_decision_inboxes_v1"} {
		out = append(out, "GRANT EXECUTE ON FUNCTION "+s+"."+name+" TO "+u+"@'%'")
	}
	return out, nil
}
