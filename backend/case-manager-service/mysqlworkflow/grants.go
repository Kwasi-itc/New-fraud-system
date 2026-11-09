package mysqlworkflow

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/mysqlaccess"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
)

func DefinerGrants(database, definer string) ([]string, error) {
	out, err := mysqlaccess.DefinerGrants(database, definer)
	if err != nil {
		return nil, err
	}
	s, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	u, err := mysqlstore.Identifier(definer)
	if err != nil {
		return nil, err
	}
	for _, table := range []string{"case_manager_workflow_locks", "case_manager_cases", "case_manager_intake_receipts", "case_manager_case_decisions", "case_manager_case_tags", "case_manager_case_events", "case_manager_outbox_events"} {
		out = append(out, "GRANT SELECT,TRIGGER ON "+s+"."+table+" TO "+u+"@`localhost`")
	}
	return out, nil
}

// RuntimeGrants belongs to the case workflow owner, never the decision API or
// dispatch worker. Decision data is read-only; receipts cannot be edited/deleted.
func RuntimeGrants(database, user string) ([]string, error) {
	s, err := mysqlstore.Identifier(database)
	if err != nil {
		return nil, err
	}
	u, err := mysqlstore.Identifier(user)
	if err != nil {
		return nil, err
	}
	account := u + "@'%'"
	var out []string
	for _, table := range []string{"decision_engine_decisions", "case_manager_inboxes", "case_manager_tags"} {
		out = append(out, "GRANT SELECT ON "+s+"."+table+" TO "+account)
	}
	for _, table := range []string{"case_manager_cases", "case_manager_intake_receipts", "case_manager_case_decisions", "case_manager_case_tags", "case_manager_workflow_locks"} {
		out = append(out, "GRANT SELECT,INSERT ON "+s+"."+table+" TO "+account)
	}
	for _, table := range []string{"case_manager_case_events", "case_manager_outbox_events"} {
		out = append(out, "GRANT INSERT ON "+s+"."+table+" TO "+account)
	}
	out = append(out, "GRANT UPDATE(lock_hash) ON "+s+".case_manager_workflow_locks TO "+account, "GRANT UPDATE(tag_id) ON "+s+".case_manager_case_tags TO "+account)
	return out, nil
}
