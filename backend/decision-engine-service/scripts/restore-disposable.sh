#!/bin/sh
# Fixed private service names and generated schema only. Never accepts a caller
# URL, database name, destination or credentials from an application environment.
set -eu
work=/backend/decision-engine-service/.restore-rehearsal
n=0
while test ! -f "$work/database"; do
  n=$((n+1))
  test "$n" -lt 240 || exit 1
  sleep 1
done
test ! -e "$work/done"
test ! -e "$work/snapshot.sql"
IFS= read -r database < "$work/database"
case "$database" in decision_target_*) ;; *) exit 1 ;; esac
case "$database" in *[!a-z0-9_]*) exit 1 ;; esac
test "${#database}" -le 64
test "$(mysql -h mysql -uroot -Nse 'SELECT VERSION()')" = 8.4.8
test "$(mysql -h mysqlrestore -uroot -Nse 'SELECT VERSION()')" = 8.4.8
# The test has installed the exact frozen owner plans into a new schema and
# removed its empty migration-ledger contents and restore-time guard triggers.
# Refuse a target containing application data. No application uses this server.
test "$(mysql -h mysqlrestore -uroot -Nse "SELECT COUNT(*) FROM \`$database\`.decision_engine_decisions")" = 0
test "$(mysql -h mysqlrestore -uroot -Nse "SELECT COUNT(*) FROM \`$database\`.case_manager_cases")" = 0
mysqldump -h mysql -uroot --single-transaction --no-create-info --no-create-db --skip-triggers \
  --hex-blob --no-tablespaces --set-gtid-purged=OFF --databases "$database" > "$work/snapshot.sql"
test -s "$work/snapshot.sql"
sha256sum "$work/snapshot.sql" > "$work/snapshot.sha256"
mysql --binary-mode=1 -h mysqlrestore -uroot < "$work/snapshot.sql"
printf 'restored\n' > "$work/done.tmp"
mv "$work/done.tmp" "$work/done"
