package mysqlreview

import (
	"github.com/Kwasi-itc/New-fraud-system/backend/case-manager-service/mysqlworkflow"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore"
	"github.com/Kwasi-itc/New-fraud-system/backend/mysqlstore/migrations"
)

func Statements(database, definer string) ([]migrations.Step, error) {
	steps, err := mysqlworkflow.CombinedStatements(database, definer)
	if err != nil {
		return nil, err
	}
	s, _ := mysqlstore.Identifier(database)
	u, _ := mysqlstore.Identifier(definer)
	steps = append(steps, migrations.Step{ID: "case_review_v001_0001", Kind: "TABLE", Name: "case_manager_resource_reviews", SQL: "CREATE TABLE " + s + `.case_manager_resource_reviews(tenant_id BINARY(16) NOT NULL,operation_id BINARY(16) NOT NULL,case_id BINARY(16) NOT NULL,resource_id BINARY(16) NOT NULL,actor_id BINARY(16) NOT NULL,status VARCHAR(16) NOT NULL CHECK(status IN('approve','decline')),findings TEXT NOT NULL,payload_hash BINARY(32) NOT NULL,result JSON NOT NULL,created_at DATETIME(6) NOT NULL,PRIMARY KEY(tenant_id,operation_id),UNIQUE KEY case_review_resource(tenant_id,resource_id),FOREIGN KEY(tenant_id,case_id) REFERENCES ` + s + `.case_manager_cases(tenant_id,id),FOREIGN KEY(tenant_id,resource_id) REFERENCES ` + s + `.decision_engine_decisions(tenant_id,id),FOREIGN KEY(actor_id) REFERENCES ` + s + `.identity_users(id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin`})
	steps = append(steps, migrations.Step{ID: "case_review_v001_0002", Kind: "TRIGGER", Name: "case_review_immutable", SQL: "CREATE DEFINER=" + u + "@`localhost` TRIGGER " + s + ".case_review_immutable BEFORE UPDATE ON " + s + ".case_manager_resource_reviews FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='review receipts are immutable'"})
	return steps, nil
}
func DefinerGrants(database, definer string) ([]string, error) {
	out, err := mysqlworkflow.DefinerGrants(database, definer)
	if err != nil {
		return nil, err
	}
	s, _ := mysqlstore.Identifier(database)
	u, _ := mysqlstore.Identifier(definer)
	return append(out, "GRANT SELECT,TRIGGER ON "+s+".case_manager_resource_reviews TO "+u+"@`localhost`"), nil
}
