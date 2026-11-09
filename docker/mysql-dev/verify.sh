#!/bin/sh
# Run inside the mysql service. Read-only and independent of application schemas.
set -eu
export MYSQL_PWD="$(cat /run/secrets/mysql_root_password)"
test "$(mysql --protocol=socket -uroot -Nse 'SELECT VERSION()')" = 8.4.8
test "$(mysql --protocol=socket -uroot -Nse 'SELECT @@character_set_server')" = utf8mb4
test "$(mysql --protocol=socket -uroot -Nse 'SELECT @@collation_server')" = utf8mb4_0900_bin
test "$(mysql --protocol=socket -uroot -Nse "SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name='fraud'")" = 1
mysql --protocol=socket -uroot --database=fraud --batch -e 'SELECT VERSION() AS version,DATABASE() AS shared_database,@@max_connections AS connection_budget;'
