#!/bin/sh
# Local owner provisioning. Credentials are mounted secrets, never command args.
set -eu
export MYSQL_PWD="$(cat /run/secrets/mysql_root_password)"
keycloak_password="$(cat /run/secrets/keycloak_mysql_password)"
case "$keycloak_password" in *[!a-f0-9]*|'') echo 'Invalid Keycloak database credential' >&2; exit 1;; esac
test "${#keycloak_password}" -eq 64
mysql_root() {
    mysql --protocol=socket --socket=/target-socket/mysqld.sock -uroot --batch "$@"
}
test "$(mysql_root --skip-column-names -e 'SELECT @@sql_generate_invisible_primary_key')" = 0 || {
    echo 'Keycloak requires sql_generate_invisible_primary_key=OFF' >&2; exit 1;
}
mysql_root <<SQL
CREATE DATABASE IF NOT EXISTS keycloak CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER IF NOT EXISTS 'fraud_keycloak'@'%' IDENTIFIED BY '${keycloak_password}';
GRANT ALL PRIVILEGES ON keycloak.* TO 'fraud_keycloak'@'%';
SQL
test "$(mysql_root --skip-column-names -e "SELECT DEFAULT_COLLATION_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME='keycloak'")" = utf8mb4_unicode_ci || {
    echo 'Unexpected existing Keycloak database collation; reconcile explicitly' >&2; exit 1;
}
unexpected_grants="$(mysql_root --skip-column-names <<'SQL'
SELECT
 (SELECT COUNT(*) FROM information_schema.USER_PRIVILEGES WHERE GRANTEE=CONCAT(QUOTE('fraud_keycloak'),'@',QUOTE('%')) AND PRIVILEGE_TYPE<>'USAGE') +
 (SELECT COUNT(*) FROM information_schema.SCHEMA_PRIVILEGES WHERE GRANTEE=CONCAT(QUOTE('fraud_keycloak'),'@',QUOTE('%')) AND TABLE_SCHEMA<>'keycloak') +
 (SELECT COUNT(*) FROM information_schema.TABLE_PRIVILEGES WHERE GRANTEE=CONCAT(QUOTE('fraud_keycloak'),'@',QUOTE('%')) AND TABLE_SCHEMA<>'keycloak') +
 (SELECT COUNT(*) FROM information_schema.COLUMN_PRIVILEGES WHERE GRANTEE=CONCAT(QUOTE('fraud_keycloak'),'@',QUOTE('%')) AND TABLE_SCHEMA<>'keycloak') +
 (SELECT COUNT(*) FROM mysql.procs_priv WHERE User='fraud_keycloak' AND Host='%' AND Db<>'keycloak') +
 (SELECT COUNT(*) FROM mysql.role_edges WHERE TO_USER='fraud_keycloak' AND TO_HOST='%');
SQL
)"
test "$unexpected_grants" = 0 || {
    echo 'Keycloak account has privileges outside its owner database; reconcile explicitly' >&2; exit 1;
}
# Verify the existing account's credential. Never silently rotate a retained user.
MYSQL_PWD="$keycloak_password" mysql --protocol=socket --socket=/target-socket/mysqld.sock -ufraud_keycloak --database=keycloak --batch -e 'SELECT 1' >/dev/null
echo 'Keycloak MySQL database and restricted owner account verified'
