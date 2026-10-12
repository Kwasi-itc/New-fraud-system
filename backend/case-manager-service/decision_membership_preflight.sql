-- Read-only inventory before migration 000012. Run against the intended shared
-- database with authorized operator access; results can contain sensitive IDs.
BEGIN READ ONLY;
SET LOCAL statement_timeout = '60s';
SELECT tenant_id, decision_id, count(*) AS memberships,
       array_agg(case_id ORDER BY case_id) AS case_ids
FROM case_manager.case_decisions
GROUP BY tenant_id, decision_id HAVING count(*) > 1;

SELECT l.tenant_id, l.id AS link_id, l.case_id, l.decision_id
FROM case_manager.case_decisions l
LEFT JOIN case_manager.cases c ON c.tenant_id=l.tenant_id AND c.id=l.case_id
LEFT JOIN core.decisions d ON d.tenant_id=l.tenant_id AND d.id=l.decision_id
WHERE c.id IS NULL OR d.id IS NULL;
COMMIT;
