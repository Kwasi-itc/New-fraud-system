#!/usr/bin/env bash
set -euo pipefail
admin=/opt/keycloak/bin/kcadm.sh
cfg=$(mktemp /tmp/fraud-ec2-admin.XXXXXX)
trap 'rm -f "$cfg"' EXIT
"$admin" config credentials --config "$cfg" --server http://localhost:8080/identity --realm master --user "$KC_BOOTSTRAP_ADMIN_USERNAME" --password "$KC_BOOTSTRAP_ADMIN_PASSWORD" >/dev/null
client=$("$admin" get clients --config "$cfg" -r fraud -q clientId=fraud-auth --fields id --format csv --noquotes)
[[ $client =~ ^[a-zA-Z0-9-]+$ ]] || { echo 'Expected exactly one fraud-auth client.' >&2; exit 1; }
"$admin" update "clients/$client" --config "$cfg" -r fraud -s "redirectUris=[\"$FRAUD_AUTH_REDIRECT_URI\"]"
"$admin" update realms/fraud --config "$cfg" -s sslRequired=external
echo 'Existing identity client callback updated; users, passwords and memberships preserved.'
