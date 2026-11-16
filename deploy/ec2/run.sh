#!/usr/bin/env bash
set -euo pipefail
umask 077
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
state="$root/.local/ec2"
action=${1:-help}
shift || true
node_image=node:22-alpine
if [[ $action == help || $action == --help ]]; then
  echo 'Usage: run.sh configure|check|up [--skip-build]|backup|verify|status|stop|auth-admin [arguments]'
  exit 0
fi

node_task() {
  docker run --rm --user "$(id -u):$(id -g)" --mount "type=bind,source=$root,target=/workspace" --workdir /workspace "$node_image" node "$@"
}
lock_state() {
  mkdir -p "$state"
  chmod 700 "$state"
  exec 9>"$state/deploy.lock"
  flock -n 9 || { echo 'Another deployment, configuration update or backup is running.' >&2; exit 1; }
}
if [[ $action == configure ]]; then
  lock_state
  node_task deploy/ec2/configure.mjs "$@"
  exit
fi
if [[ ! -f "$state/compose.env" ]]; then
  echo 'Run configure with --domain, --email and --case-tenants first.' >&2
  exit 1
fi
case "$action" in up|backup|stop|auth-admin) lock_state ;; esac
compose=(docker compose --project-directory "$root" --env-file "$state/compose.env" -p fraud-ec2 -f "$root/docker-compose.yml" -f "$root/docker-compose.gateway.yml" -f "$root/deploy/ec2/compose.yml")
node_task deploy/ec2/configure.mjs --check
"${compose[@]}" config --quiet

probe() {
  docker run --rm --network fraud-ec2_internal --user "$(id -u):$(id -g)" --mount "type=bind,source=$root,target=/workspace,readonly" --workdir /workspace "$node_image" node deploy/ec2/probe.mjs "$@"
}
backup() {
  local stamp
  stamp=$(date -u +%Y%m%dT%H%M%SZ)
  mkdir -p "$state/backups"
  chmod 700 "$state/backups"
  "${compose[@]}" exec -T postgres pg_dump -U fraud -d fraud -Fc >"$state/backups/$stamp.dump.partial"
  mv -- "$state/backups/$stamp.dump.partial" "$state/backups/$stamp.dump"
  tar -czf "$state/backups/$stamp-identity.tgz.partial" -C "$root" .local/auth-provider .local/ec2/compose.env
  mv -- "$state/backups/$stamp-identity.tgz.partial" "$state/backups/$stamp-identity.tgz"
  echo 'Private database and identity backup saved under .local/ec2/backups/.'
}
case "$action" in
  check)
    echo 'EC2 configuration and Compose model are valid.'
    ;;
  up)
    [[ $# == 0 || ( $# == 1 && $1 == --skip-build ) ]] || { echo 'Usage: up [--skip-build]' >&2; exit 1; }
    if [[ ${1:-} != --skip-build ]]; then
      # Avoid simultaneous Go/Next builds consuming the entire EC2 memory budget.
      COMPOSE_BAKE=false "${compose[@]}" --parallel 1 build
    fi
    "${compose[@]}" up -d --wait --wait-timeout 120 postgres
    backup
    # Router/TLS/identity precede auth's strict HTTPS OIDC discovery.
    "${compose[@]}" up -d --wait --wait-timeout 300 keycloak api-gateway tls-edge
    "${compose[@]}" exec -T keycloak /bin/bash -s <"$root/deploy/ec2/keycloak.sh"
    probe --identity-only --wait
    "${compose[@]}" up -d --wait --wait-timeout 300
    probe --wait
    echo 'EC2 deployment is ready. Use the configured HTTPS domain.'
    ;;
  backup)
    backup
    ;;
  verify)
    probe
    ;;
  status)
    "${compose[@]}" ps
    ;;
  auth-admin)
    "${compose[@]}" exec -T auth-service /app/admin "$@"
    ;;
  stop)
    "${compose[@]}" stop
    ;;
  *)
    echo 'Usage: run.sh configure|check|up [--skip-build]|backup|verify|status|stop|auth-admin [arguments]' >&2
    exit 1
    ;;
esac
